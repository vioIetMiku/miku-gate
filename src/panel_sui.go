package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SUIPanel struct {
	mu                             sync.Mutex
	dir, origin, base, token, host string
	http                           *http.Client
	state                          suiBindings
}

type suiBindings struct {
	Hosts    map[string]string `json:"hosts"`
	Remarks  map[string]string `json:"remarks,omitempty"`
	Disabled map[string]bool   `json:"disabled"`
}

func suiInstalled() bool {
	for _, path := range []string{"/usr/local/s-ui/db/s-ui.db", "/usr/local/s-ui/s-ui.db"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// S-UI exposes token-authenticated persistent APIs. The root installer owns
// this local integration; it reads settings and reuses an existing token.
// If no token exists, one private token is created once and S-UI reloads it.
func DetectSUI(dir string) (*SUIPanel, error) {
	if !suiInstalled() {
		return nil, fmt.Errorf("未检测到本机S-UI")
	}
	const read = `import json,os,sqlite3,sys,secrets,time
paths=['/usr/local/s-ui/db/s-ui.db','/usr/local/s-ui/s-ui.db']
path=next(p for p in paths if os.path.isfile(p))
c=sqlite3.connect('file:'+path+'?mode=rw',uri=True,timeout=5)
s=dict(c.execute('SELECT key,value FROM settings'))
r=c.execute('SELECT token FROM tokens WHERE expiry=0 OR expiry>? ORDER BY id DESC LIMIT 1',(int(time.time()),)).fetchone()
created=False
if not r:
 uid=c.execute('SELECT id FROM users ORDER BY id LIMIT 1').fetchone()[0]
 backup=os.path.join(sys.argv[1],'sui-before-token.db')
 if not os.path.exists(backup):
  b=sqlite3.connect(backup);c.backup(b);b.close();os.chmod(backup,0o600)
 token=secrets.token_urlsafe(32)
 c.execute('INSERT INTO tokens(desc,token,expiry,user_id) VALUES(?,?,?,?)',('Miku local adapter',token,0,uid));c.commit();created=True
else:token=r[0]
c.close()
print(json.dumps({'port':int(s.get('webPort','2095')),'base':s.get('webPath','/app/'),'domain':s.get('webDomain',''),'cert':s.get('webCertFile',''),'token':token,'created':created}))`
	output, err := cmdOutput(exec.Command("python3", "-B", "-c", read, dir))
	if err != nil {
		return nil, fmt.Errorf("无法读取S-UI本机设置与API令牌")
	}
	var settings struct {
		Port                      int
		Base, Domain, Cert, Token string
		Created                   bool
	}
	if json.Unmarshal(output, &settings) != nil || settings.Port < 1 || settings.Port > 65535 || settings.Token == "" {
		return nil, fmt.Errorf("S-UI设置不完整")
	}
	if settings.Created {
		command := exec.Command("systemctl", "restart", "s-ui.service")
		if _, err := exec.LookPath("systemctl"); err != nil {
			command = exec.Command("rc-service", "s-ui", "restart")
		}
		if _, err := cmdCombined(command); err != nil {
			return nil, fmt.Errorf("已创建本机API令牌，S-UI重载失败，请运行 s-ui restart")
		}
	}
	base := "/" + strings.Trim(settings.Base, "/") + "/"
	if base == "//" {
		base = "/"
	}
	scheme := "http"
	transport := &http.Transport{Proxy: nil}
	if settings.Cert != "" {
		raw, err := os.ReadFile(settings.Cert)
		if err != nil {
			return nil, fmt.Errorf("无法读取S-UI管理证书")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("S-UI管理证书格式无效")
		}
		name := settings.Domain
		if name == "" {
			block, _ := pem.Decode(raw)
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("S-UI管理证书无效")
			}
			if len(certificate.DNSNames) > 0 {
				name = certificate.DNSNames[0]
			} else {
				name = certificate.Subject.CommonName
			}
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: name}
		scheme = "https"
	}
	s := &SUIPanel{dir: dir, host: settings.Domain, origin: fmt.Sprintf("%s://127.0.0.1:%d", scheme, settings.Port), base: base, token: settings.Token, http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("本机面板不接受跳转") }}, state: suiBindings{Hosts: map[string]string{}, Remarks: map[string]string{}, Disabled: map[string]bool{}}}
	if raw, err := os.ReadFile(filepath.Join(dir, "sui-bindings.json")); err == nil {
		if json.Unmarshal(raw, &s.state) != nil {
			return nil, fmt.Errorf("S-UI绑定记录无效")
		}
	}
	if s.state.Remarks == nil {
		s.state.Remarks = map[string]string{}
	}
	if s.state.Hosts == nil {
		s.state.Hosts = map[string]string{}
	}
	if s.state.Disabled == nil {
		s.state.Disabled = map[string]bool{}
	}
	for i := 0; i < 5; i++ {
		if _, err := s.load(); err == nil {
			return s, nil
		}
		if !settings.Created {
			break
		}
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("S-UI本机API不可达，请确认面板已启动")
}

func (s *SUIPanel) Kind() string     { return "s-ui" }
func (s *SUIPanel) Describe() string { return "接管本机 S-UI / sing-box" }
func (s *SUIPanel) Close()           {}
func (s *SUIPanel) call(path string, form url.Values) (json.RawMessage, error) {
	method := "GET"
	var body io.Reader
	if form != nil {
		method = "POST"
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequest(method, s.origin+s.base+"apiv2/"+path, body)
	if err != nil {
		return nil, err
	}
	if s.host != "" {
		request.Host = s.host
	}
	request.Header.Set("Token", s.token)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("S-UI接口连接失败")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil || len(raw) >= 8<<20 || response.StatusCode != 200 {
		return nil, fmt.Errorf("S-UI接口响应异常")
	}
	var value struct {
		Success bool            `json:"success"`
		Obj     json.RawMessage `json:"obj"`
		Msg     string          `json:"msg"`
	}
	if json.Unmarshal(raw, &value) != nil || !value.Success {
		return nil, fmt.Errorf("S-UI拒绝此操作，请检查所选节点格式")
	}
	return value.Obj, nil
}
func (s *SUIPanel) load() (map[string]json.RawMessage, error) {
	raw, err := s.call("load", nil)
	if err != nil {
		return nil, err
	}
	var value map[string]json.RawMessage
	err = json.Unmarshal(raw, &value)
	return value, err
}
func (s *SUIPanel) save(object, action string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.call("save", url.Values{"object": {object}, "action": {action}, "data": {string(raw)}})
	return err
}
func suiObjects(raw json.RawMessage) []map[string]any {
	var values []map[string]any
	json.Unmarshal(raw, &values)
	return values
}
func suiObject(value any) map[string]any {
	if v, ok := value.(map[string]any); ok {
		return v
	}
	var result map[string]any
	if v, ok := value.(string); ok {
		json.Unmarshal([]byte(v), &result)
	}
	if result == nil {
		result = map[string]any{}
	}
	return result
}
func suiNumber(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
func suiString(value any) string { valueString, _ := value.(string); return valueString }
func suiCopy(value any) map[string]any {
	raw, _ := json.Marshal(value)
	var result map[string]any
	json.Unmarshal(raw, &result)
	return result
}
func suiMember(value any, id int) bool {
	values, _ := value.([]any)
	for _, v := range values {
		if suiNumber(v) == id {
			return true
		}
	}
	return false
}
func (s *SUIPanel) native(snapshot map[string]json.RawMessage, raw map[string]any) *nativeInbound {
	id := suiNumber(raw["id"])
	tag := suiString(raw["tag"])
	ib := &nativeInbound{ID: id, Port: suiNumber(raw["listen_port"]), Protocol: suiString(raw["type"]), Network: "tcp", Security: "none", Remark: tag, Enable: !s.state.Disabled[tag], BoundTo: s.state.Hosts[tag]}
	if remark := s.state.Remarks[tag]; remark != "" {
		ib.Remark = remark
	}
	if transport := suiObject(raw["transport"]); len(transport) > 0 {
		ib.Network = suiString(transport["type"])
		ib.Path = suiString(transport["path"])
		if ib.Network == "grpc" {
			ib.Path = suiString(transport["service_name"])
		}
	}
	for _, value := range suiObjects(snapshot["tls"]) {
		if suiNumber(value["id"]) != suiNumber(raw["tls_id"]) {
			continue
		}
		tlsValue := suiObject(value["server"])
		if tlsValue["enabled"] != true {
			continue
		}
		ib.Security = "tls"
		ib.TLS = &tlsConfig{ServerName: suiString(tlsValue["server_name"]), CertFile: suiString(tlsValue["certificate_path"]), KeyFile: suiString(tlsValue["key_path"])}
		if reality := suiObject(tlsValue["reality"]); reality["enabled"] == true {
			ib.Security = "reality"
			handshake := suiObject(reality["handshake"])
			key := suiString(reality["private_key"])
			pub := ""
			if bytes, err := base64KeyPublic(key); err == nil {
				pub = bytes
			}
			short := []string{}
			if list, ok := reality["short_id"].([]any); ok {
				for _, v := range list {
					short = append(short, suiString(v))
				}
			}
			ib.Reality = &realityConfig{Dest: fmt.Sprintf("%s:%d", suiString(handshake["server"]), suiNumber(handshake["server_port"])), ServerNames: []string{ib.TLS.ServerName}, PrivateKey: key, PublicKey: pub, ShortIDs: short, Fingerprint: "chrome"}
		}
	}
	for _, client := range suiObjects(snapshot["clients"]) {
		if !suiMember(client["inbounds"], id) {
			continue
		}
		profile := suiObject(suiObject(client["config"])[ib.Protocol])
		ib.Clients = append(ib.Clients, nativeClient{Email: suiString(client["name"]), ID: suiString(profile["uuid"]), Password: suiString(profile["password"]), Flow: suiString(profile["flow"]), Enable: client["enable"] == true})
	}
	return ib
}
func (s *SUIPanel) Inbounds(live map[string]bool) ([]Inbound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.load()
	if err != nil {
		return nil, err
	}
	values := []Inbound{}
	for _, raw := range suiObjects(snapshot["inbounds"]) {
		ib := s.native(snapshot, raw)
		values = append(values, Inbound{Stack: ib.protocolStack(), ID: ib.ID, Port: ib.Port, Protocol: ib.Protocol, Remark: ib.Remark, Enable: ib.Enable, Tag: suiString(raw["tag"]), BoundTo: ib.BoundTo, BoundUp: live[ib.BoundTo]})
	}
	return values, nil
}
func (s *SUIPanel) InboundDetail(id int, host string) (*InboundDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.load()
	if err != nil {
		return nil, err
	}
	for _, raw := range suiObjects(snapshot["inbounds"]) {
		if suiNumber(raw["id"]) != id {
			continue
		}
		ib := s.native(snapshot, raw)
		detail := &InboundDetail{Inbound: Inbound{ID: id, Port: ib.Port, Protocol: ib.Protocol, Remark: ib.Remark, Enable: ib.Enable, Tag: suiString(raw["tag"]), BoundTo: ib.BoundTo}, Listen: suiString(raw["listen"]), Network: ib.Network, TLS: ib.Security}
		for _, client := range ib.Clients {
			id := client.ID
			if ib.Protocol == "trojan" {
				id = client.Password
			}
			detail.Clients = append(detail.Clients, ClientInfo{Email: client.Email, ID: id, Enable: client.Enable})
			if nativeProtocols[ib.Protocol] {
				detail.Links = append(detail.Links, shareLink(ib, client, host))
			}
		}
		return detail, nil
	}
	return nil, fmt.Errorf("S-UI节点不存在")
}
func (s *SUIPanel) InboundLinks(ids []int, host string) ([]string, error) {
	links := []string{}
	for _, id := range ids {
		detail, err := s.InboundDetail(id, host)
		if err != nil {
			return links, err
		}
		links = append(links, detail.Links...)
	}
	return links, nil
}
func (s *SUIPanel) persist() error {
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, "sui-bindings.json")
	if err = os.WriteFile(path+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func (s *SUIPanel) resync(tunnels []*Tunnel) error {
	snapshot, err := s.load()
	if err != nil {
		return err
	}
	var config map[string]any
	if json.Unmarshal(snapshot["config"], &config) != nil {
		return fmt.Errorf("S-UI基础配置无效")
	}
	route := suiObject(config["route"])
	existing, _ := route["rules"].([]any)
	kept := []any{}
	for _, rule := range existing {
		r := suiObject(rule)
		if suiString(r["miku_group"]) == "" {
			if !strings.HasPrefix(suiString(r["outbound"]), xuiTagPrefix) {
				ins, _ := r["inbound"].([]any)
				managed := false
				if len(ins) == 1 && r["action"] == "reject" {
					_, managed = s.state.Hosts[suiString(ins[0])]
					if s.state.Disabled[suiString(ins[0])] {
						managed = true
					}
				}
				if !managed {
					kept = append(kept, rule)
				}
			}
		}
	}
	live := map[string]bool{}
	outbounds := suiObjects(snapshot["outbounds"])
	for _, t := range tunnels {
		if t.Status != "up" {
			continue
		}
		key := sanitizeTag(t.Node.HostName)
		live[key] = true
		cred := t.credential()
		out := map[string]any{"type": "socks", "tag": tunnelTag(t), "server": "127.0.0.1", "server_port": t.Port, "version": "5", "username": cred.User, "password": cred.Pass}
		action := "new"
		for _, old := range outbounds {
			if old["tag"] == out["tag"] {
				out["id"] = old["id"]
				action = "edit"
			}
		}
		if err = s.save("outbounds", action, out); err != nil {
			return err
		}
	}
	rules := []any{}
	for _, in := range suiObjects(snapshot["inbounds"]) {
		tag := suiString(in["tag"])
		host := s.state.Hosts[tag]
		if host == "" && !s.state.Disabled[tag] {
			continue
		}
		rule := map[string]any{"inbound": []string{tag}, "action": "reject", "method": "drop"}
		if !s.state.Disabled[tag] && live[host] {
			rule = map[string]any{"inbound": []string{tag}, "action": "route", "outbound": xuiTagPrefix + host}
		}
		rules = append(rules, rule)
	}
	route["rules"] = append(rules, kept...)
	config["route"] = route
	if err = s.save("config", "edit", config); err != nil {
		return err
	}
	return s.persist()
}
func (s *SUIPanel) Bind(tag, host string, tunnels []*Tunnel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if host == "" {
		delete(s.state.Hosts, tag)
	} else {
		ready := false
		for _, t := range tunnels {
			if t.Node.HostName == host && t.Status == "up" {
				ready = true
			}
		}
		if !ready {
			return fmt.Errorf("先等所选出口连通")
		}
		s.state.Hosts[tag] = sanitizeTag(host)
	}
	return s.resync(tunnels)
}
func (s *SUIPanel) Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for tag, host := range s.state.Hosts {
		if host == sanitizeTag(oldHost) {
			s.state.Hosts[tag] = sanitizeTag(target.Node.HostName)
		}
	}
	return s.resync(tunnels)
}
func (s *SUIPanel) ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error {
	return s.OnTunnelsChanged(tunnels)
}
func (s *SUIPanel) OnTunnelsChanged(tunnels []*Tunnel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resync(tunnels)
}
func (s *SUIPanel) UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.load()
	if err != nil {
		return err
	}
	for _, raw := range suiObjects(snapshot["inbounds"]) {
		if suiNumber(raw["id"]) != id {
			continue
		}
		tag := suiString(raw["tag"])
		if patch.Port != nil && *patch.Port != suiNumber(raw["listen_port"]) {
			if !portAvailable(*patch.Port) {
				return fmt.Errorf("端口已占用")
			}
			raw["listen_port"] = *patch.Port
		}
		if patch.Remark != nil {
			remark := strings.TrimSpace(*patch.Remark)
			if remark != "" {
				s.state.Remarks[tag] = remark
			}
		}
		if err = s.save("inbounds", "edit", raw); err != nil {
			return err
		}
		if patch.Enable != nil {
			s.state.Disabled[tag] = !*patch.Enable
		}
		return s.resync(tunnels)
	}
	return fmt.Errorf("节点不存在")
}
func (s *SUIPanel) mutateClient(id int, email, operation string, tunnels []*Tunnel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.load()
	if err != nil {
		return err
	}
	var inbound map[string]any
	for _, v := range suiObjects(snapshot["inbounds"]) {
		if suiNumber(v["id"]) == id {
			inbound = v
		}
	}
	if inbound == nil {
		return fmt.Errorf("节点不存在")
	}
	protocol := suiString(inbound["type"])
	if operation == "new" {
		if email == "" {
			email = fmt.Sprintf("miku-%d-%s", id, randomHex(3))
		}
		profile := map[string]any{"uuid": newUUID()}
		if protocol == "trojan" {
			profile = map[string]any{"password": randomHex(16)}
		} else if protocol == "vless" {
			for _, v := range suiObjects(snapshot["clients"]) {
				if suiMember(v["inbounds"], id) {
					flow := suiString(suiObject(suiObject(v["config"])[protocol])["flow"])
					if flow != "" {
						profile["flow"] = flow
					}
					break
				}
			}
		}
		if err = s.save("clients", "new", map[string]any{"name": email, "enable": true, "inbounds": []int{id}, "config": map[string]any{protocol: profile}, "volume": 0, "expiry": 0}); err != nil {
			return err
		}
		return s.resync(tunnels)
	}
	for _, client := range suiObjects(snapshot["clients"]) {
		if suiString(client["name"]) != email || !suiMember(client["inbounds"], id) {
			continue
		}
		if operation == "del" {
			bindings, _ := client["inbounds"].([]any)
			kept := []any{}
			for _, v := range bindings {
				if suiNumber(v) != id {
					kept = append(kept, v)
				}
			}
			if len(kept) == 0 {
				err = s.save("clients", "del", suiNumber(client["id"]))
			} else {
				client["inbounds"] = kept
				err = s.save("clients", "edit", client)
			}
		} else {
			config := suiObject(client["config"])
			profile := suiObject(config[protocol])
			if protocol == "trojan" {
				profile["password"] = randomHex(16)
			} else {
				profile["uuid"] = newUUID()
			}
			config[protocol] = profile
			client["config"] = config
			err = s.save("clients", "edit", client)
		}
		if err != nil {
			return err
		}
		return s.resync(tunnels)
	}
	return fmt.Errorf("客户端不存在")
}
func (s *SUIPanel) AddClient(id int, email string, tunnels []*Tunnel) error {
	return s.mutateClient(id, email, "new", tunnels)
}
func (s *SUIPanel) DeleteClient(id int, email string, tunnels []*Tunnel) error {
	return s.mutateClient(id, email, "del", tunnels)
}
func (s *SUIPanel) ResetClient(id int, email string, tunnels []*Tunnel) error {
	return s.mutateClient(id, email, "reset", tunnels)
}
func (s *SUIPanel) DeleteInbounds(ids []int, tunnels []*Tunnel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.load()
	if err != nil {
		return err
	}
	for _, id := range ids {
		for _, raw := range suiObjects(snapshot["inbounds"]) {
			if suiNumber(raw["id"]) == id {
				if err = s.save("inbounds", "del", id); err != nil {
					return err
				}
				delete(s.state.Hosts, suiString(raw["tag"]))
				delete(s.state.Disabled, suiString(raw["tag"]))
				delete(s.state.Remarks, suiString(raw["tag"]))
			}
		}
	}
	return s.resync(tunnels)
}
func (s *SUIPanel) CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.load()
	if err != nil {
		return nil, err
	}
	used := externalUsedPorts()
	for _, v := range suiObjects(snapshot["inbounds"]) {
		used[suiNumber(v["listen_port"])] = true
	}
	normalized, err := normalizeInboundSpec(spec, used)
	if err != nil {
		return nil, err
	}
	ib := &nativeInbound{ID: 1, Port: normalized.Port, Protocol: normalized.Protocol, Network: normalized.Network, Path: normalized.Path, Host: normalized.Host, Security: normalized.Security, Remark: normalized.Remark, Enable: true}
	if ib.Security == "tls" {
		ib.TLS, err = buildTLS(s.dir, spec)
	}
	if ib.Security == "reality" {
		ib.Reality, err = generatedReality(spec)
	}
	if err != nil {
		return nil, err
	}
	ib.Clients = []nativeClient{{Email: fmt.Sprintf("miku-%d", ib.Port), ID: newUUID(), Password: randomHex(16), Flow: normalized.Flow, Enable: true}}
	config, err := buildSingBoxConfig([]*nativeInbound{ib}, nil)
	if err != nil {
		return nil, err
	}
	raw := suiObject(config["inbounds"].([]any)[0])
	delete(raw, "users")
	tag := normalized.Remark
	raw["tag"] = tag
	for _, v := range suiObjects(snapshot["inbounds"]) {
		if v["tag"] == tag {
			tag = fmt.Sprintf("%s-%d", tag, ib.Port)
		}
	}
	raw["tag"] = tag
	if tlsValue, ok := raw["tls"]; ok {
		delete(raw, "tls")
		name := fmt.Sprintf("miku-%d", ib.Port)
		if err = s.save("tls", "new", map[string]any{"name": name, "server": tlsValue, "client": map[string]any{"enabled": true, "server_name": spec.ServerName}}); err != nil {
			return nil, err
		}
		snapshot, err = s.load()
		if err != nil {
			return nil, err
		}
		for _, tls := range suiObjects(snapshot["tls"]) {
			if tls["name"] == name {
				raw["tls_id"] = tls["id"]
			}
		}
	}
	if err = s.save("inbounds", "new", raw); err != nil {
		return nil, err
	}
	snapshot, err = s.load()
	if err != nil {
		return nil, err
	}
	id := 0
	for _, v := range suiObjects(snapshot["inbounds"]) {
		if v["tag"] == tag {
			id = suiNumber(v["id"])
		}
	}
	if id == 0 {
		return nil, fmt.Errorf("新节点未回读")
	}
	profile := map[string]any{"uuid": ib.Clients[0].ID}
	if ib.Protocol == "trojan" {
		profile = map[string]any{"password": ib.Clients[0].Password}
	} else if normalized.Flow != "" {
		profile["flow"] = normalized.Flow
	}
	if err = s.save("clients", "new", map[string]any{"name": ib.Clients[0].Email, "enable": true, "inbounds": []int{id}, "config": map[string]any{ib.Protocol: profile}, "volume": 0, "expiry": 0}); err != nil {
		return nil, err
	}
	return &CreatedInbound{ID: id, Port: ib.Port, Protocol: ib.Protocol, Remark: tag, Network: ib.Network, Security: ib.Security}, nil
}
func (s *SUIPanel) CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.load()
	if err != nil {
		return nil, err
	}
	var template map[string]any
	used := externalUsedPorts()
	for _, raw := range suiObjects(snapshot["inbounds"]) {
		used[suiNumber(raw["listen_port"])] = true
		if suiNumber(raw["id"]) == templateID {
			template = raw
		}
	}
	if template == nil {
		return nil, fmt.Errorf("S-UI模板不存在")
	}
	ports := []int{}
	for _, host := range hosts {
		var tunnel *Tunnel
		for _, t := range tunnels {
			if t.Node.HostName == host && t.Status == "up" {
				tunnel = t
			}
		}
		if tunnel == nil {
			continue
		}
		port, err := freeRandomPort(used)
		if err != nil {
			return ports, err
		}
		used[port] = true
		raw := suiCopy(template)
		delete(raw, "id")
		tag := fmt.Sprintf("miku-%s-%d", suiString(raw["type"]), port)
		raw["tag"] = tag
		raw["listen_port"] = port
		if err = s.save("inbounds", "new", raw); err != nil {
			return ports, err
		}
		after, err := s.load()
		if err != nil {
			return ports, err
		}
		newID := 0
		for _, v := range suiObjects(after["inbounds"]) {
			if v["tag"] == tag {
				newID = suiNumber(v["id"])
			}
		}
		if newID == 0 {
			return ports, fmt.Errorf("新节点未回读")
		}
		for _, client := range suiObjects(snapshot["clients"]) {
			if !suiMember(client["inbounds"], templateID) {
				continue
			}
			copy := suiCopy(client)
			delete(copy, "id")
			copy["name"] = suiString(client["name"]) + "-" + strconv.Itoa(port)
			copy["inbounds"] = []int{newID}
			if err = s.save("clients", "new", copy); err != nil {
				return ports, err
			}
		}
		s.state.Hosts[tag] = sanitizeTag(host)
		ports = append(ports, port)
	}
	if err = s.resync(tunnels); err != nil {
		return ports, err
	}
	return ports, nil
}
