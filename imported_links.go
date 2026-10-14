package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

type ImportedLink struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Server    string `json:"server"`
	Port      int    `json:"port"`
	Link      string `json:"link"`
	InboundID int    `json:"inbound_id,omitempty"`
	Slot      int    `json:"slot,omitempty"`
}

func sameLocalConfig(a, b subscriptionNode) bool {
	a.Name, b.Name = "", ""
	a.Server, b.Server = "", ""
	if a.Encryption == "" {
		a.Encryption = "none"
	}
	if b.Encryption == "" {
		b.Encryption = "none"
	}
	return reflect.DeepEqual(a, b)
}

// Do not turn pasted remote links into local entrances. Match both address
// and full connection material against an entrance already on this VPS.
func (m *Manager) resolveLocalLink(item ImportedLink) (ImportedLink, error) {
	want, err := parseSubscriptionNode(item.Link)
	if err != nil {
		return ImportedLink{}, err
	}
	settings := getWebSettings()
	public := publishingHost()
	localAddress := func(expected string) bool {
		for _, candidate := range []string{expected, public, settings.PublicIP, settings.PublicHost} {
			if candidate != "" && strings.EqualFold(strings.TrimSuffix(want.Server, "."), strings.TrimSuffix(candidate, ".")) {
				return true
			}
		}
		return strings.EqualFold(want.Server, hostPublicIP())
	}
	if want.Protocol == "socks5" || want.Protocol == "http" {
		if !localAddress(public) || want.Security == "tls" {
			return ImportedLink{}, fmt.Errorf("请粘贴本 VPS 生成的 SOCKS5 / HTTP 链接")
		}
		for _, t := range m.Tunnels() {
			cred := t.credential()
			if t.Port == want.Port && t.Status == "up" && cred.User == want.Username && cred.Pass == want.Password {
				u := url.URL{Scheme: want.Protocol, Host: net.JoinHostPort(public, strconv.Itoa(t.Port)), User: url.UserPassword(cred.User, cred.Pass), Fragment: want.Name}
				resolved, err := parseImportedLink(u.String())
				resolved.Slot = t.Slot
				return resolved, err
			}
		}
		return ImportedLink{}, fmt.Errorf("本 VPS 未找到此代理端口或凭据，可能已停止或重置")
	}
	p, err := openPanel()
	if err != nil {
		return ImportedLink{}, fmt.Errorf("无法核对本机入口：%w", err)
	}
	list, err := p.Inbounds(nil)
	if err != nil {
		return ImportedLink{}, err
	}
	for _, ib := range list {
		if !ib.Enable || ib.Port != want.Port || ib.Protocol != want.Protocol {
			continue
		}
		links, err := p.InboundLinks([]int{ib.ID}, public)
		if err != nil {
			return ImportedLink{}, fmt.Errorf("此面板暂不能核验分享链接：%w", err)
		}
		for _, link := range links {
			actual, err := parseSubscriptionNode(link)
			if err == nil && localAddress(actual.Server) && sameLocalConfig(want, actual) {
				resolved, err := parseImportedLink(link)
				resolved.InboundID = ib.ID
				return resolved, err
			}
		}
	}
	return ImportedLink{}, fmt.Errorf("未匹配本 VPS 的启用入口，请检查服务器地址、端口、凭据和传输参数")
}

// Recheck stored bookmarks after deletion, credential resets and upgrades.
// Legacy remote imports are preserved on disk but no longer shown/exported.
func (m *Manager) verifiedImportedLinks() []ImportedLink {
	items, _ := m.readImportedLinks()
	valid := []ImportedLink{}
	seen := map[string]bool{}
	for _, item := range items {
		if current, err := m.resolveLocalLink(item); err == nil && !seen[current.ID] {
			seen[current.ID] = true
			current.ID = item.ID
			valid = append(valid, current)
		}
	}
	return valid
}

var importedLinksMu sync.Mutex

func (m *Manager) readImportedLinks() ([]ImportedLink, error) {
	raw, err := os.ReadFile(filepath.Join(m.workDir, "imported-links.json"))
	if os.IsNotExist(err) {
		return []ImportedLink{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 4<<20 {
		return nil, fmt.Errorf("导入文件过大")
	}
	var links []ImportedLink
	err = json.Unmarshal(raw, &links)
	return links, err
}

func parseImportedLink(raw string) (ImportedLink, error) {
	n, err := parseSubscriptionNode(raw)
	if err != nil {
		return ImportedLink{}, err
	}
	if n.Protocol == "vless" || n.Protocol == "vmess" {
		uuid, err := hex.DecodeString(strings.ReplaceAll(n.UUID, "-", ""))
		if err != nil || len(uuid) != 16 {
			return ImportedLink{}, fmt.Errorf("UUID格式无效")
		}
	}
	n.Name = "" // Names do not make a duplicate endpoint into a new node.
	key, _ := json.Marshal(n)
	sum := sha256.Sum256(key)
	parsed, _ := parseSubscriptionNode(raw)
	if parsed.Protocol == "vmess" {
		raw = parsed.vmessLink()
	}
	return ImportedLink{ID: hex.EncodeToString(sum[:16]), Name: parsed.Name, Protocol: parsed.Protocol, Server: parsed.Server, Port: parsed.Port, Link: raw}, nil
}

func (m *Manager) importLinks(raw string, removeID string) (int, []ImportedLink, error) {
	importedLinksMu.Lock()
	defer importedLinksMu.Unlock()
	list, err := m.readImportedLinks()
	if err != nil {
		return 0, nil, err
	}
	seen := map[string]bool{}
	for _, item := range list {
		seen[item.ID] = true
	}
	additions := []ImportedLink{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		item, err := parseImportedLink(line)
		if err != nil {
			return 0, nil, err
		}
		item, err = m.resolveLocalLink(item)
		if err != nil {
			return 0, nil, err
		}
		if !seen[item.ID] {
			seen[item.ID] = true
			additions = append(additions, item)
		}
	}
	if removeID != "" {
		kept := []ImportedLink{}
		for _, item := range list {
			if item.ID != removeID {
				kept = append(kept, item)
			}
		}
		list = kept
	}
	list = append(list, additions...)
	blob, err := json.MarshalIndent(list, "", "  ")
	if err != nil || len(blob) > 4<<20 {
		return 0, nil, fmt.Errorf("无法保存导入链接")
	}
	path := filepath.Join(m.workDir, "imported-links.json")
	if err := os.WriteFile(path+".tmp", blob, 0600); err != nil {
		return 0, nil, err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return 0, nil, err
	}
	return len(additions), list, nil
}

func apiImportedLinks(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			links := m.verifiedImportedLinks()
			writeJSON(w, 200, map[string]any{"links": links})
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "只接受GET或POST"})
			return
		}
		var in struct {
			Links  string `json:"links"`
			Remove string `json:"remove"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in) != nil {
			writeJSON(w, 400, map[string]string{"error": "请求格式无效"})
			return
		}
		if strings.TrimSpace(in.Links) == "" && in.Remove == "" {
			writeJSON(w, 400, map[string]string{"error": "请先粘贴节点分享链接"})
			return
		}
		added, links, err := m.importLinks(in.Links, in.Remove)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"added": added, "links": links})
	}
}
