package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type CertificateChoice struct {
	ID         string    `json:"id"`
	Domains    []string  `json:"domains"`
	Issuer     string    `json:"issuer"`
	Expires    time.Time `json:"expires"`
	SelfSigned bool      `json:"self_signed"`
	Source     string    `json:"source"`
	CertFile   string    `json:"-"`
	KeyFile    string    `json:"-"`
}
type RegisteredDomain struct {
	Domain     string    `json:"domain"`
	CertFile   string    `json:"cert_file"`
	KeyFile    string    `json:"key_file"`
	SourceCert string    `json:"source_cert,omitempty"`
	SourceKey  string    `json:"source_key,omitempty"`
	Added      time.Time `json:"added"`
}
type DomainRegistry struct {
	mu        sync.Mutex
	operation sync.Mutex
	issuing   bool
	dir       string
	server    *webServer
	manager   *Manager
	values    []RegisteredDomain
}

func newDomainRegistry(dir string, server *webServer, m *Manager) *DomainRegistry {
	d := &DomainRegistry{dir: dir, server: server, manager: m, values: []RegisteredDomain{}}
	if raw, err := os.ReadFile(filepath.Join(dir, "domains.json")); err == nil && len(raw) < 1<<20 {
		if json.Unmarshal(raw, &d.values) != nil {
			d.values = []RegisteredDomain{}
		}
	}
	return d
}
func (d *DomainRegistry) save() error {
	raw, err := json.MarshalIndent(d.values, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(d.dir, "domains.json")
	if err = os.WriteFile(path+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func validDomain(raw string) bool {
	return net.ParseIP(raw) == nil && validPublishingHost(raw) && strings.Contains(raw, ".") && !strings.Contains(raw, "*")
}

func readCertificateFile(path string, key bool) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("证书文件不可读")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<10 || info.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("证书文件权限或大小异常")
	}
	if owner, ok := info.Sys().(*syscall.Stat_t); !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("证书文件不属于当前服务账号")
	}
	if key && info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("证书私钥需要私有权限600")
	}
	return os.ReadFile(resolved)
}
func certificateChoice(certFile, keyFile, source string) (CertificateChoice, error) {
	certRaw, err := readCertificateFile(certFile, false)
	if err != nil {
		return CertificateChoice{}, err
	}
	keyRaw, err := readCertificateFile(keyFile, true)
	if err != nil {
		return CertificateChoice{}, err
	}
	pair, err := tls.X509KeyPair(certRaw, keyRaw)
	if err != nil || len(pair.Certificate) == 0 {
		return CertificateChoice{}, fmt.Errorf("证书与私钥不匹配")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return CertificateChoice{}, err
	}
	if time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return CertificateChoice{}, fmt.Errorf("证书未生效或已过期")
	}
	domains := append([]string{}, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		domains = append(domains, ip.String())
	}
	if len(domains) == 0 && leaf.Subject.CommonName != "" {
		domains = append(domains, leaf.Subject.CommonName)
	}
	if len(domains) == 0 {
		return CertificateChoice{}, fmt.Errorf("证书没有域名或IP")
	}
	id := sha256.Sum256([]byte(filepath.Clean(certFile) + "\x00" + filepath.Clean(keyFile)))
	self := leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil && bytesEqual(leaf.RawIssuer, leaf.RawSubject)
	return CertificateChoice{ID: hex.EncodeToString(id[:12]), Domains: domains, Issuer: leaf.Issuer.CommonName, Expires: leaf.NotAfter, SelfSigned: self, Source: source, CertFile: certFile, KeyFile: keyFile}, nil
}
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (d *DomainRegistry) catalogLocked() []CertificateChoice {
	choices := []CertificateChoice{}
	seen := map[string]bool{}
	add := func(cert, key, source string) {
		if cert == "" || key == "" {
			return
		}
		value, err := certificateChoice(cert, key, source)
		if err == nil && !seen[value.ID] {
			seen[value.ID] = true
			choices = append(choices, value)
		}
	}
	settings := getWebSettings()
	add(settings.TLSCert, settings.TLSKey, "当前管理证书")
	for _, entry := range d.values {
		add(entry.CertFile, entry.KeyFile, "已绑定："+entry.Domain)
	}
	if store, err := loadNativeStore(d.dir); err == nil {
		for _, inbound := range store.Inbounds {
			if inbound.TLS != nil {
				add(inbound.TLS.CertFile, inbound.TLS.KeyFile, "入口："+inbound.Remark)
			}
		}
	}
	for _, root := range []string{"/etc/letsencrypt/live", "/root/.acme.sh"} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if root == "/etc/letsencrypt/live" {
				add(filepath.Join(path, "fullchain.pem"), filepath.Join(path, "privkey.pem"), "Certbot")
			} else {
				name := strings.TrimSuffix(entry.Name(), "_ecc")
				add(filepath.Join(path, "fullchain.cer"), filepath.Join(path, name+".key"), "acme.sh")
			}
		}
	}
	// Reuse the panel's configured certificate without changing its files,
	// listener or renewal configuration.
	if cert, key := xuiCertificatePaths(); cert != "" && key != "" {
		add(cert, key, "3x-ui 管理证书")
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Source < choices[j].Source })
	return choices
}
func (d *DomainRegistry) Catalog() []CertificateChoice {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.catalogLocked()
}
func certificateSupports(choice CertificateChoice, domain string) bool {
	raw, err := readCertificateFile(choice.CertFile, false)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return false
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	return err == nil && leaf.VerifyHostname(domain) == nil
}

func (d *DomainRegistry) resolveTLS(id, domain string) (CertificateChoice, string, error) {
	choices := d.Catalog()
	domain = strings.ToLower(strings.TrimSpace(domain))
	if id != "" {
		for _, choice := range choices {
			if choice.ID == id {
				if domain == "" {
					for _, name := range choice.Domains {
						if !strings.Contains(name, "*") {
							domain = name
							break
						}
					}
				}
				if domain != "" && certificateSupports(choice, domain) {
					return choice, domain, nil
				}
				return CertificateChoice{}, "", fmt.Errorf("选择的域名不在证书有效范围内")
			}
		}
		return CertificateChoice{}, "", fmt.Errorf("证书已变更，请刷新后重新选择")
	}
	preferred := getWebSettings().PublicHost
	if domain != "" {
		preferred = domain
	}
	for _, choice := range choices {
		if preferred != "" && certificateSupports(choice, preferred) {
			return choice, preferred, nil
		}
	}
	if len(choices) == 1 {
		for _, name := range choices[0].Domains {
			if !strings.Contains(name, "*") {
				return choices[0], name, nil
			}
		}
	}
	if len(choices) > 1 {
		return CertificateChoice{}, "", fmt.Errorf("检测到多个证书，请选择域名与证书")
	}
	return CertificateChoice{}, "", fmt.Errorf("未检测到可用证书；请先绑定域名，或明确选择自签/手动证书")
}

func (d *DomainRegistry) keepCertificate(choice CertificateChoice, domain string) (RegisteredDomain, error) {
	if !validDomain(domain) || !certificateSupports(choice, domain) {
		return RegisteredDomain{}, fmt.Errorf("域名与证书不匹配")
	}
	certRaw, err := readCertificateFile(choice.CertFile, false)
	if err != nil {
		return RegisteredDomain{}, err
	}
	keyRaw, err := readCertificateFile(choice.KeyFile, true)
	if err != nil {
		return RegisteredDomain{}, err
	}
	root := filepath.Join(d.dir, "certificates", domain)
	if err = os.MkdirAll(root, 0700); err != nil {
		return RegisteredDomain{}, err
	}
	cert, key := filepath.Join(root, "certificate.pem"), filepath.Join(root, "private.key")
	if err = os.WriteFile(cert, certRaw, 0600); err != nil {
		return RegisteredDomain{}, err
	}
	if err = os.WriteFile(key, keyRaw, 0600); err != nil {
		return RegisteredDomain{}, err
	}
	entry := RegisteredDomain{Domain: domain, CertFile: cert, KeyFile: key, Added: time.Now()}
	if choice.CertFile != cert {
		entry.SourceCert = choice.CertFile
		entry.SourceKey = choice.KeyFile
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	previous := append([]RegisteredDomain{}, d.values...)
	found := false
	for i := range d.values {
		if d.values[i].Domain == domain {
			if entry.SourceCert == "" {
				entry.SourceCert = d.values[i].SourceCert
				entry.SourceKey = d.values[i].SourceKey
			}
			d.values[i] = entry
			found = true
		}
	}
	if !found {
		d.values = append(d.values, entry)
	}
	if err = d.save(); err != nil {
		d.values = previous
		return RegisteredDomain{}, err
	}
	if d.dir == "/var/lib/miku" {
		if _, err := os.Stat("/usr/local/lib/miku/maintain.py"); err == nil {
			go func() {
				_ = cmdRun(exec.Command("python3", "-B", "/usr/local/lib/miku/maintain.py", "certificate-automation"))
			}()
		}
	}
	return entry, nil
}

func publishedTLSHost(name, fallback string) string {
	if certificateRegistry == nil {
		return fallback
	}
	d := certificateRegistry
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, entry := range d.values {
		if entry.Domain == name {
			return name
		}
	}
	return fallback
}

func (d *DomainRegistry) activate(entry RegisteredDomain) error {
	next := getWebSettings()
	next.PublicHost = entry.Domain
	next.TLSCert = entry.CertFile
	next.TLSKey = entry.KeyFile
	next.CertificateDomain = entry.Domain
	return d.server.applyWebSettings(next)
}

type domainRequest struct {
	Action        string `json:"action"`
	Domain        string `json:"domain"`
	CertificateID string `json:"certificate_id"`
	Email         string `json:"email"`
	Management    bool   `json:"management"`
	Webroot       string `json:"webroot"`
}

func xuiCertificatePaths() (string, string) {
	const binary = "/usr/local/x-ui/x-ui"
	if _, err := os.Stat(binary); err != nil {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, binary, "setting", "-getCert").Output()
	if err != nil {
		return "", ""
	}
	text := reANSI.ReplaceAllString(string(raw), "")
	cert := reXUICert.FindStringSubmatch(text)
	key := regexp.MustCompile(`(?m)^key:[^\S\r\n]*(\S+)`).FindStringSubmatch(text)
	if len(cert) == 2 && len(key) == 2 {
		return cert[1], key[1]
	}
	return "", ""
}

func certificateCommandArgs(input domainRequest) ([]string, error) {
	mode := []string{"--standalone"}
	if input.Webroot != "" {
		root := filepath.Clean(input.Webroot)
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil || !filepath.IsAbs(root) || root == "/" || resolved != root {
			return nil, fmt.Errorf("请填写实际存在的网站根目录，不能使用根目录或符号链接")
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("网站根目录不存在")
		}
		mode = []string{"--webroot", "-w", root}
	}
	args := append([]string{"certonly"}, mode...)
	return append(args, "--preferred-challenges", "http", "--non-interactive", "--agree-tos", "--email", input.Email, "--cert-name", input.Domain, "-d", input.Domain), nil
}

func (d *DomainRegistry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			choices := d.Catalog()
			d.mu.Lock()
			registered := []map[string]any{}
			for _, entry := range d.values {
				registered = append(registered, map[string]any{"domain": entry.Domain, "added": entry.Added})
			}
			d.mu.Unlock()
			cfg := getWebSettings()
			writeJSON(w, 200, map[string]any{"certificates": choices, "registered": registered, "current": cfg.PublicHost, "management_url": cfg.origin(publishingHost()) + currentBasePath() + "/", "https": cfg.TLSCert != ""})
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "请用POST"})
			return
		}
		if !d.operation.TryLock() {
			writeJSON(w, 409, map[string]string{"error": "域名操作进行中，请稍后重试"})
			return
		}
		defer d.operation.Unlock()
		d.mu.Lock()
		busy := d.issuing
		d.mu.Unlock()
		if busy {
			writeJSON(w, 409, map[string]string{"error": "证书签发任务进行中，请等待完成"})
			return
		}
		var input domainRequest
		if json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&input) != nil {
			writeJSON(w, 400, map[string]string{"error": "请求格式错误"})
			return
		}
		input.Domain = strings.ToLower(strings.TrimSpace(input.Domain))
		if input.Action != "unbind" && !validDomain(input.Domain) {
			writeJSON(w, 400, map[string]string{"error": "请填写有效域名，不带协议、端口或路径"})
			return
		}
		if input.Action == "unbind" || input.Action == "bind-http" {
			next := getWebSettings()
			next.PublicHost = next.PublicIP
			if input.Action == "bind-http" {
				next.PublicHost = input.Domain
			}
			next.TLSCert = ""
			next.TLSKey = ""
			next.CertificateDomain = ""
			if err := d.server.applyWebSettings(next); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{"url": next.origin(next.PublicHost) + currentBasePath() + "/", "management": true})
			return
		}
		if input.Action == "remove" {
			d.mu.Lock()
			old := append([]RegisteredDomain{}, d.values...)
			next := []RegisteredDomain{}
			for _, entry := range d.values {
				if entry.Domain != input.Domain {
					next = append(next, entry)
				}
			}
			d.values = next
			err := d.save()
			if err != nil {
				d.values = old
			}
			d.mu.Unlock()
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": "无法保存域名列表"})
				return
			}
			writeJSON(w, 200, map[string]string{"ok": "已从绑定列表移除；已有入口和证书保留"})
			return
		}
		// A matching, unexpired certificate avoids ACME and port 80 entirely.
		if input.Action == "issue" {
			for _, choice := range d.Catalog() {
				if !choice.SelfSigned && certificateSupports(choice, input.Domain) {
					input.Action, input.CertificateID = "bind", choice.ID
					break
				}
			}
		}
		if input.Action == "issue" {
			if !strings.Contains(input.Email, "@") || strings.ContainsAny(input.Email, "\r\n\x00") || len(input.Email) > 254 {
				writeJSON(w, 400, map[string]string{"error": "申请证书需要有效联系邮箱"})
				return
			}
			args, err := certificateCommandArgs(input)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			d.mu.Lock()
			issuing := d.issuing
			if !issuing {
				d.issuing = true
			}
			d.mu.Unlock()
			if issuing {
				writeJSON(w, 409, map[string]string{"error": "已有证书签发任务，请等待完成"})
				return
			}
			if input.Webroot == "" && !portAvailable(80) {
				d.mu.Lock()
				d.issuing = false
				d.mu.Unlock()
				writeJSON(w, 400, map[string]string{"error": "80端口正由其他服务使用。可选择已有证书，填写现有网站根目录验证，或选择仅绑定HTTP管理域名；无需停止其他面板。"})
				return
			}
			job := d.manager.jobs.New("绑定域名 "+input.Domain, []string{"申请证书", "保存域名", "启用管理域名"})
			go func() {
				defer job.Finish()
				defer func() { d.mu.Lock(); d.issuing = false; d.mu.Unlock() }()
				d.operation.Lock()
				defer d.operation.Unlock()
				job.Set(0, "running", "等待ACME验证，请确认域名解析及80端口放行")
				if _, err := exec.LookPath("certbot"); err != nil {
					prepare := exec.Command("python3", "-B", "/usr/local/lib/miku/maintain.py", "certificate-prepare")
					if err := cmdRun(prepare); err != nil {
						job.Set(0, "failed", "certbot自动安装失败，请用 miku repair 检查依赖")
						return
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
				defer cancel()
				cmd := exec.CommandContext(ctx, "certbot", args...)
				if err := cmdRun(cmd); err != nil {
					job.Set(0, "failed", "签发失败；请检查DNS、80端口及certbot日志")
					return
				}
				job.Set(0, "ok", "证书已签发")
				choice, err := certificateChoice(filepath.Join("/etc/letsencrypt/live", input.Domain, "fullchain.pem"), filepath.Join("/etc/letsencrypt/live", input.Domain, "privkey.pem"), "Certbot")
				if err != nil {
					job.Set(1, "failed", err.Error())
					return
				}
				entry, err := d.keepCertificate(choice, input.Domain)
				if err != nil {
					job.Set(1, "failed", err.Error())
					return
				}
				job.Set(1, "ok", input.Domain)
				if input.Management {
					if err = d.activate(entry); err != nil {
						job.Set(2, "failed", err.Error())
						return
					}
					url := getWebSettings().origin(entry.Domain) + currentBasePath() + "/"
					job.SetResultURL(url)
					job.Set(2, "ok", "管理域名已启用，请打开新的登录地址")
				} else {
					job.Set(2, "ok", "域名可用于TLS入口，管理地址保留")
				}
			}()
			result := map[string]string{"job": job.ID()}
			if input.Management {
				next := getWebSettings()
				next.PublicHost = input.Domain
				result["next_url"] = "https://" + net.JoinHostPort(input.Domain, fmt.Sprint(next.Port)) + currentBasePath() + "/"
			}
			writeJSON(w, 202, result)
			return
		}
		if input.Action != "bind" {
			writeJSON(w, 400, map[string]string{"error": "未知域名操作"})
			return
		}
		choice, domain, err := d.resolveTLS(input.CertificateID, input.Domain)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		entry, err := d.keepCertificate(choice, domain)
		if err == nil && input.Management {
			err = d.activate(entry)
		}
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		cfg := getWebSettings()
		writeJSON(w, 200, map[string]any{"domain": domain, "url": cfg.origin(cfg.PublicHost) + currentBasePath() + "/", "management": input.Management})
	}
}
