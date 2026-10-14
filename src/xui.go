package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// XUI 对接本机的 3x-ui 面板。
// 面板端口与 webBasePath 都是安装时随机生成的，这里从 x-ui 命令行读出来，
// API token 同样由 `x-ui setting -getApiToken` 提供（没有时它会自动生成一个）。
type XUI struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	BasePath string `json:"base_path"`
	Scheme   string `json:"scheme"`
	token    string
	client   *http.Client
	// workDir 是 fanout 的工作目录，新建 TLS 入站时自签证书落在这里。
	workDir string
}

// base 返回访问面板用的前缀。
func (x *XUI) base() string {
	return fmt.Sprintf("%s://%s:%d%s", x.Scheme, x.Host, x.Port, x.BasePath)
}

func (x *XUI) Kind() string { return "3x-ui" }

func (x *XUI) Describe() string {
	return fmt.Sprintf("接管本机 3x-ui 面板（%s:%d）", x.Host, x.Port)
}

const (
	// 面板主程序，用来读设置和取 API token
	xuiBinary = "/usr/local/x-ui/x-ui"
	// 交互式管理脚本，第 11 项会打印面板的对外访问地址
	xuiMenu = "/usr/bin/x-ui"
)

// 每次调用 `x-ui setting -getApiToken` 面板都会新生成一个 token 且不回收，
// 重启多了会把面板的 api_tokens 表撑爆。所以：进程内缓存复用，
// 跨重启则把 token 落盘（<workDir>/xui-token），下次先验证旧的还能不能用，
// 能用就不再新建。
var (
	cachedToken   string
	cachedTokenMu sync.Mutex
)

// xuiTokenFile 是 token 落盘的文件名，放在 fanout 工作目录下。
const xuiTokenFile = "xui-token"

var (
	// 值一律限定在本行之内取：`\s*` 会跨过换行，字段为空时会把下一行的内容当成值。
	reXUIPort = regexp.MustCompile(`(?m)^port:[^\S\r\n]*(\d+)`)
	reXUIBase = regexp.MustCompile(`(?m)^webBasePath:[^\S\r\n]*(\S+)`)
	// 只认 "apiToken: xxx" 这一行，避免匹配到提示文字里的长单词
	reXUIToken = regexp.MustCompile(`(?m)^apiToken:[^\S\r\n]*([A-Za-z0-9]+)`)
	// 面板开了 TLS 时 setting -show 打印 "Panel is secure with SSL"，
	// 没开则打印 "Warning: Panel is not secure with SSL"——后者包含前者，必须先排除。
	reXUISSLOff = regexp.MustCompile(`(?i)panel is not secure with ssl`)
	reXUISSLOn  = regexp.MustCompile(`(?i)panel is secure with ssl`)
	// 兜底：证书路径非空也说明启用了 TLS
	reXUICert = regexp.MustCompile(`(?m)^cert:[^\S\r\n]*(\S+)`)
	// x-ui 菜单第 11 项打印的 Access URL，绑了域名时给的是域名而不是 IP
	reXUIAccessURL = regexp.MustCompile(`Access URL:\s*(https?)://([^:/\s]+):(\d+)(\S*)`)
	reANSI         = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

// xuiSSLFromSettings 判断 `x-ui setting -show` 的输出说的是"开了 SSL"还是"没开"。
// 第三个返回值表示这段输出里到底有没有提到 SSL，没提到时调用方才去看证书。
func xuiSSLFromSettings(text string) (on, stated bool) {
	if reXUISSLOff.MatchString(text) {
		return false, true
	}
	if reXUISSLOn.MatchString(text) {
		return true, true
	}
	return false, false
}

// xuiCertConfigured 判断 `x-ui setting -getCert` 的输出里证书路径是否非空。
func xuiCertConfigured(text string) bool {
	return reXUICert.MatchString(text)
}

// panelAccess 从 `x-ui` 的「View Current Settings」里取面板地址。
//
// 那段逻辑已经处理好了绑定域名的情况：有证书就用证书里的域名，没有才退回公网 IP。
// 比我们自己拼 127.0.0.1 靠谱，尤其是面板启用了 TLS 时证书不会签给回环地址。
func panelAccess() (scheme, host string, ok bool) {
	cmd := exec.Command(xuiMenu)
	cmd.Stdin = strings.NewReader("11\n\n0\n")
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return "", "", false
	}
	// 输出带 ANSI 颜色码，先剥掉再匹配
	m := reXUIAccessURL.FindStringSubmatch(stripANSI(string(out)))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// stripANSI 去掉终端颜色控制码。
func stripANSI(s string) string {
	return reANSI.ReplaceAllString(s, "")
}

// DetectXUI 探测本机 3x-ui。未安装或未运行时返回错误。
// workDir 用于落盘/复用 API token；传空则退回每次新建的旧行为。
func DetectXUI(workDir string) (*XUI, error) {
	if !xuiRunning() {
		return nil, fmt.Errorf("本机未安装或未运行 3x-ui")
	}

	out, err := exec.Command(xuiBinary, "setting", "-show").Output()
	if err != nil {
		return nil, fmt.Errorf("读取面板设置失败: %w", err)
	}
	text := string(out)

	scheme := "http"
	host := "127.0.0.1"
	// 优先信 x-ui 自己给出的地址（可能是域名）
	if sc, h, ok := panelAccess(); ok {
		scheme, host = sc, h
	} else if on, stated := xuiSSLFromSettings(text); stated {
		// 面板自己说了开没开，直接采信，不必再查证书
		if on {
			scheme = "https"
		}
	} else if certOut, err := exec.Command(xuiBinary, "setting", "-getCert").Output(); err == nil {
		if xuiCertConfigured(string(certOut)) {
			scheme = "https"
		}
	}

	pm := reXUIPort.FindStringSubmatch(text)
	bm := reXUIBase.FindStringSubmatch(text)
	if pm == nil || bm == nil {
		return nil, fmt.Errorf("无法从面板设置中解析端口或路径")
	}
	var port int
	fmt.Sscanf(pm[1], "%d", &port)

	newXUI := func(token string) *XUI {
		return &XUI{
			Host:     host,
			Port:     port,
			BasePath: strings.TrimSuffix(bm[1], "/"),
			Scheme:   scheme,
			token:    token,
			client:   localClient(),
			workDir:  workDir,
		}
	}

	cachedTokenMu.Lock()
	defer cachedTokenMu.Unlock()
	if cachedToken != "" {
		return newXUI(cachedToken), nil
	}

	// 先试盘上存的 token：验证还能用就复用，避免每次启动都新建一个。
	if workDir != "" {
		if saved := readSavedToken(workDir); saved != "" {
			x := newXUI(saved)
			if x.tokenValid() {
				cachedToken = saved
				return x, nil
			}
		}
	}

	// 没有可用 token，这条命令会自动生成一个
	tokOut, err := exec.Command(xuiBinary, "setting", "-getApiToken").Output()
	if err != nil {
		return nil, fmt.Errorf("获取 API token 失败: %w", err)
	}
	tm := reXUIToken.FindStringSubmatch(string(tokOut))
	if tm == nil {
		return nil, fmt.Errorf("未能取得 API token")
	}
	token := tm[1]

	cachedToken = token
	if workDir != "" {
		saveToken(workDir, token)
	}
	return newXUI(token), nil
}

// tokenValid 用一次只读调用验证当前 token 还能用。
func (x *XUI) tokenValid() bool {
	_, err := x.get("panel/api/inbounds/list")
	return err == nil
}

// readSavedToken 读回上次落盘的 token，没有就返回空串。
func readSavedToken(workDir string) string {
	blob, err := os.ReadFile(filepath.Join(workDir, xuiTokenFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(blob))
}

// saveToken 把 token 落盘（0600），失败只记日志不阻断。
func saveToken(workDir, token string) {
	path := filepath.Join(workDir, xuiTokenFile)
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		log.Printf("保存 API token 失败（不影响本次运行）: %v", err)
	}
}

// localClient 用于访问本机面板。
//
// 面板启用 TLS 时证书通常签给公网 IP 或域名，而我们走的是 127.0.0.1，
// 校验必然失败。这是同一台机器上的进程间调用，不经过网络，
// 没有中间人风险，所以跳过证书校验。
func localClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //#nosec G402 -- 仅用于 127.0.0.1
		},
	}
}

// post 调用面板 API。v3.5.0 里 Bearer token 只对 /panel/api/ 前缀生效。
func (x *XUI) post(path string, form url.Values) ([]byte, error) {
	return x.call(http.MethodPost, path, form)
}

// get 调用面板的只读 API。inbounds/list 是 GET。
func (x *XUI) get(path string) ([]byte, error) {
	return x.call(http.MethodGet, path, nil)
}

func (x *XUI) call(method, path string, form url.Values) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/%s", x.base(), strings.TrimPrefix(path, "/"))

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+x.token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := x.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 %s 失败: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("调用 %s 返回 HTTP %d", path, resp.StatusCode)
	}

	var envelope struct {
		Success bool            `json:"success"`
		Msg     string          `json:"msg"`
		Obj     json.RawMessage `json:"obj"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("解析 %s 响应失败: %w", path, err)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("面板返回失败: %s", envelope.Msg)
	}
	return envelope.Obj, nil
}

// xrayConfig 是 /panel/api/xray/ 返回的结构。
// 注意 obj 本身是一个 JSON 字符串，要二次解析。
type xrayConfig struct {
	OutboundTestURL string          `json:"outboundTestUrl"`
	XraySetting     json.RawMessage `json:"xraySetting"`
}

// loadXray 读取当前 Xray 配置模板。
func (x *XUI) loadXray() (map[string]any, string, error) {
	obj, err := x.post("panel/api/xray/", nil)
	if err != nil {
		return nil, "", err
	}

	// obj 是被再次编码成字符串的 JSON
	var inner string
	if err := json.Unmarshal(obj, &inner); err != nil {
		return nil, "", fmt.Errorf("解析 Xray 配置外层失败: %w", err)
	}
	var cfg xrayConfig
	if err := json.Unmarshal([]byte(inner), &cfg); err != nil {
		return nil, "", fmt.Errorf("解析 Xray 配置失败: %w", err)
	}

	var setting map[string]any
	if err := json.Unmarshal(cfg.XraySetting, &setting); err != nil {
		return nil, "", fmt.Errorf("解析 xraySetting 失败: %w", err)
	}
	return setting, cfg.OutboundTestURL, nil
}

// saveXray 写回 Xray 配置模板并让面板重启 Xray。
func (x *XUI) saveXray(setting map[string]any, testURL string) error {
	blob, err := json.Marshal(setting)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("xraySetting", string(blob))
	if testURL != "" {
		form.Set("outboundTestUrl", testURL)
	}
	if _, err := x.post("panel/api/xray/update", form); err != nil {
		return err
	}

	// 只写模板不够：面板要重载 Xray 才会用新的 outbounds 与 routing 生成运行配置，
	// 否则路由改动看起来保存成功了，实际流量还按旧规则走。
	if _, err := x.post("panel/api/server/restartXrayService", nil); err != nil {
		return fmt.Errorf("配置已保存但重载 Xray 失败: %w", err)
	}
	return nil
}

// Inbound 是面板里已有的一个入站。
type Inbound struct {
	Stack    string `json:"stack"`
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Remark   string `json:"remark"`
	Enable   bool   `json:"enable"`
	Tag      string `json:"tag"`      // Xray 里的 inboundTag
	BoundTo  string `json:"bound_to"` // 已绑定的节点主机名，空表示未绑定
	BoundUp  bool   `json:"bound_up"` // 绑定的节点当前是否有运行中的隧道
}

// Inbounds 列出面板里已有的入站。
func (x *XUI) Inbounds(live map[string]bool) ([]Inbound, error) {
	obj, err := x.get("panel/api/inbounds/list")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID       int             `json:"id"`
		Port     int             `json:"port"`
		Protocol string          `json:"protocol"`
		Remark   string          `json:"remark"`
		Enable   bool            `json:"enable"`
		Tag      string          `json:"tag"`
		Stream   json.RawMessage `json:"streamSettings"`
		Settings json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(obj, &raw); err != nil {
		return nil, fmt.Errorf("解析入站列表失败: %w", err)
	}

	bound, err := x.boundInbounds()
	if err != nil {
		return nil, err
	}

	out := make([]Inbound, 0, len(raw))
	for _, r := range raw {
		// 3x-ui persists the authoritative Xray tag and returns it in this API.
		// Do not reconstruct it from the transport: recent 3x-ui versions may
		// keep a "-tcp" tag for WebSocket inbounds, so reconstruction produces
		// a routing rule that can never match the running Xray inbound.
		tag := resolvedInboundTag(r.Tag, r.Port, r.Stream)
		out = append(out, Inbound{
			Stack: inboundStackJSON(r.Protocol, r.Stream, r.Settings),
			ID:    r.ID, Port: r.Port, Protocol: r.Protocol,
			Remark: r.Remark, Enable: r.Enable,
			Tag: tag, BoundTo: bound[tag], BoundUp: live[bound[tag]],
		})
	}
	return out, nil
}

// inboundTag 复原 3x-ui 给入站生成的 Xray tag，格式是 in-<端口>-<网络>。
// streamSettings 在不同接口下有时是 JSON 对象、有时是被编码过的字符串。
func inboundTag(port int, streamSettings json.RawMessage) string {
	network := "tcp"
	if len(streamSettings) > 0 {
		raw := streamSettings
		var asString string
		if json.Unmarshal(raw, &asString) == nil {
			raw = json.RawMessage(asString)
		}
		var st struct {
			Network string `json:"network"`
		}
		if json.Unmarshal(raw, &st) == nil && st.Network != "" {
			network = st.Network
		}
	}
	return fmt.Sprintf("in-%d-%s", port, network)
}

// resolvedInboundTag uses the authoritative tag returned by 3x-ui and only
// reconstructs it for compatibility with older API responses that omit tag.
func resolvedInboundTag(apiTag string, port int, streamSettings json.RawMessage) string {
	if apiTag != "" {
		return apiTag
	}
	return inboundTag(port, streamSettings)
}

// 出站与路由规则统一带这个前缀，便于识别与清理，不碰用户手工加的条目。
const xuiTagPrefix = "miku-"

// tunnelTag 用节点主机名而非槽位号做标识：槽位在 fanout 重启后会重新分配，
// 用它做 tag 会让已有的入站绑定悄悄串到别的节点上。
func tunnelTag(t *Tunnel) string {
	return xuiTagPrefix + sanitizeTag(t.Node.HostName)
}

// sanitizeTag 把主机名收敛成安全的 tag 片段。
func sanitizeTag(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

// boundInbounds 返回 inboundTag -> 隧道槽位 的当前绑定关系。
func (x *XUI) boundInbounds() (map[string]string, error) {
	setting, _, err := x.loadXray()
	if err != nil {
		return nil, err
	}
	bound := map[string]string{}
	routing, _ := setting["routing"].(map[string]any)
	if routing == nil {
		return bound, nil
	}
	rules, _ := routing["rules"].([]any)
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		tag, _ := m["outboundTag"].(string)
		if !strings.HasPrefix(tag, xuiTagPrefix) {
			continue
		}
		host := strings.TrimPrefix(tag, xuiTagPrefix)
		// 早期版本用槽位号做 tag，这类规则在重启后必然指向错误的节点，直接忽略
		if isAllDigits(host) {
			continue
		}
		for _, it := range toStringSlice(m["inboundTag"]) {
			bound[it] = host
		}
	}
	return bound, nil
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Bind 把某个入站的流量导向指定隧道。slot 传 0 表示解绑，恢复直连。
//
// 只动 miku- 前缀的出站与规则，用户手工配置的条目原样保留。
func (x *XUI) Bind(inboundTag string, hostname string, tunnels []*Tunnel) error {
	var target *Tunnel
	if hostname != "" {
		for _, t := range tunnels {
			if t.Node.HostName == hostname {
				target = t
				break
			}
		}
		if target == nil {
			return fmt.Errorf("节点 %s 没有运行中的隧道", hostname)
		}
		if target.Status != "up" {
			return fmt.Errorf("节点 %s 的隧道还没连通（当前 %s）", hostname, target.Status)
		}
	}

	live := map[string]bool{}
	for _, t := range tunnels {
		if t.Status == "up" {
			live[sanitizeTag(t.Node.HostName)] = true
		}
	}
	current, err := x.Inbounds(live)
	if err != nil {
		return err
	}
	knownTags := map[string]bool{}
	for _, ib := range current {
		knownTags[ib.Tag] = true
	}

	setting, testURL, err := x.loadXray()
	if err != nil {
		return err
	}

	x.syncOutbounds(setting, tunnels)

	routing, _ := setting["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
	}
	rules, _ := routing["rules"].([]any)

	// 先摘掉这个入站现有的 fanout 绑定，再按需要重新加一条
	cleaned := make([]any, 0, len(rules)+1)
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			cleaned = append(cleaned, r)
			continue
		}
		outTag, _ := m["outboundTag"].(string)
		if !strings.HasPrefix(outTag, xuiTagPrefix) {
			cleaned = append(cleaned, r)
			continue
		}
		// 顺便丢掉不再存在的入站标签（如换过端口后残留的旧规则）
		remain := []any{}
		for _, it := range toStringSlice(m["inboundTag"]) {
			if it != inboundTag && knownTags[it] {
				remain = append(remain, it)
			}
		}
		if len(remain) > 0 {
			m["inboundTag"] = remain
			cleaned = append(cleaned, m)
		}
	}

	if target != nil {
		cleaned = append(cleaned, map[string]any{
			"type":        "field",
			"inboundTag":  []any{inboundTag},
			"outboundTag": tunnelTag(target),
		})
	}

	routing["rules"] = cleaned
	setting["routing"] = routing
	return x.saveXray(setting, testURL)
}

// syncOutbounds 让 miku- 出站与当前已连通的隧道保持一致。
func (x *XUI) syncOutbounds(setting map[string]any, tunnels []*Tunnel) {
	outbounds, _ := setting["outbounds"].([]any)
	kept := make([]any, 0, len(outbounds))
	for _, ob := range outbounds {
		m, ok := ob.(map[string]any)
		if !ok {
			kept = append(kept, ob)
			continue
		}
		tag, _ := m["tag"].(string)
		if !strings.HasPrefix(tag, xuiTagPrefix) {
			forceIPv4(m)
			kept = append(kept, ob)
		}
	}
	for _, t := range tunnels {
		if t.Status != "up" {
			continue
		}
		kept = append(kept, map[string]any{
			"tag":      tunnelTag(t),
			"protocol": "socks",
			"settings": map[string]any{
				"servers": []any{socksServerJSON(t)},
			},
		})
	}
	setting["outbounds"] = kept
}

// CloneToTunnels 以某个入站为模板，为每条指定隧道复制一个入站并绑定到对应出口。
//
// 复制时必须换掉端口、备注，以及客户端的 id/email —— 这些在面板里要求唯一。
// 返回新建入站的端口列表。
func (x *XUI) CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error) {
	raw, err := x.rawInbound(templateID)
	if err != nil {
		return nil, err
	}

	byHost := map[string]*Tunnel{}
	for _, t := range tunnels {
		byHost[t.Node.HostName] = t
	}

	used, err := x.usedPorts()
	if err != nil {
		return nil, err
	}

	emails, err := clientEmails(raw)
	if err != nil {
		return nil, err
	}

	// 别名撞了客户端会丢节点，先把面板里已有的备注收进来避让
	takenRemarks := map[string]bool{}
	if list, lerr := x.Inbounds(nil); lerr == nil {
		for _, ib := range list {
			takenRemarks[strings.TrimSpace(ib.Remark)] = true
		}
	}

	created := []int{}
	for _, host := range hosts {
		t := byHost[host]
		if t == nil || t.Status != "up" {
			continue
		}

		port, err := freeRandomPort(used)
		if err != nil {
			return created, err
		}
		used[port] = true

		clone, err := cloneInboundPayload(raw, port, t)
		if err != nil {
			return created, err
		}
		if remark, ok := clone["remark"].(string); ok {
			unique := uniqueRemark(remark, takenRemarks)
			clone["remark"] = unique
			takenRemarks[unique] = true
		}
		newID, err := x.addInbound(clone)
		if err != nil {
			return created, fmt.Errorf("复制到端口 %d 失败: %w", port, err)
		}
		if len(emails) > 0 {
			if err := x.attachClients(emails, newID); err != nil {
				return created, err
			}
		}
		created = append(created, port)

		// Read the tag assigned by 3x-ui instead of guessing it from the
		// template transport. This matters for WS inbounds whose persisted tag
		// may still use the "tcp" suffix.
		newRaw, err := x.rawInbound(newID)
		if err != nil {
			return created, fmt.Errorf("读取端口 %d 的入站标签失败: %w", port, err)
		}
		newTag, _ := newRaw["tag"].(string)
		if newTag == "" {
			newTag = inboundTagOf(port, raw)
		}
		if err := x.Bind(newTag, t.Node.HostName, tunnels); err != nil {
			return created, fmt.Errorf("端口 %d 绑定失败: %w", port, err)
		}
	}
	return created, nil
}

// rawInbound 取回某个入站的原始 JSON，用作复制模板。
func (x *XUI) rawInbound(id int) (map[string]any, error) {
	obj, err := x.get("panel/api/inbounds/list")
	if err != nil {
		return nil, err
	}
	var list []map[string]any
	if err := json.Unmarshal(obj, &list); err != nil {
		return nil, fmt.Errorf("解析入站列表失败: %w", err)
	}
	for _, m := range list {
		if int(toFloat(m["id"])) == id {
			return m, nil
		}
	}
	return nil, fmt.Errorf("入站 %d 不存在", id)
}

func toFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

// usedPorts 收集面板里已占用的入站端口。
func (x *XUI) usedPorts() (map[int]bool, error) {
	list, err := x.Inbounds(nil)
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	for _, ib := range list {
		used[ib.Port] = true
	}
	return used, nil
}

func inboundTagOf(port int, template map[string]any) string {
	stream, _ := json.Marshal(template["streamSettings"])
	return inboundTag(port, stream)
}

// cloneInboundPayload 按模板构造一个新入站的提交体。
func cloneInboundPayload(tpl map[string]any, port int, t *Tunnel) (map[string]any, error) {
	settings, err := asObject(tpl["settings"])
	if err != nil {
		return nil, fmt.Errorf("解析模板 settings 失败: %w", err)
	}

	label := exitLabel(t)

	// 客户端不重新生成：建成空入站后用 attach 把模板的客户端挂过来，
	// 这样同一套 UUID 能走所有出口，客户端那边只改端口即可。
	settings["clients"] = []any{}

	stream, err := asObject(tpl["streamSettings"])
	if err != nil {
		return nil, fmt.Errorf("解析模板 streamSettings 失败: %w", err)
	}
	sniff, err := asObject(tpl["sniffing"])
	if err != nil {
		sniff = map[string]any{"enabled": true, "destOverride": []any{"http", "tls"}}
	}

	return map[string]any{
		"enable":         true,
		"remark":         label,
		"listen":         fmt.Sprint(orEmpty(tpl["listen"])),
		"port":           port,
		"protocol":       fmt.Sprint(tpl["protocol"]),
		"expiryTime":     0,
		"total":          0,
		"settings":       mustJSON(settings),
		"streamSettings": mustJSON(stream),
		"sniffing":       mustJSON(sniff),
		"allocate":       mustJSON(map[string]any{}),
	}, nil
}

// exitLabel 给复制出来的入站起个好认的名字：国旗 + 中文国名 + 出口 IP 末段。
// 同一地区可能有多条隧道，带上末段才能区分。
func exitLabel(t *Tunnel) string {
	place := nodeLabel(t.Node)

	suffix := t.Node.HostName
	if t.ExitIP != "" {
		if i := strings.LastIndex(t.ExitIP, "."); i >= 0 {
			suffix = t.ExitIP[i+1:]
		} else {
			suffix = t.ExitIP
		}
	}

	if place == "" {
		return suffix
	}
	// 末段留着做区分：同一个国家可能开好几条出口，名字重了客户端会认不清，
	// mihomo 那边甚至直接要求节点名唯一
	return place + " " + suffix
}

// asObject 兼容字段是对象或是被编码成字符串的两种情况。
func asObject(v any) (map[string]any, error) {
	switch t := v.(type) {
	case map[string]any:
		return t, nil
	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(t), &m); err != nil {
			return nil, err
		}
		return m, nil
	}
	return nil, fmt.Errorf("无法解析为对象")
}

func orEmpty(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// addInbound 通过面板 API 新建一个入站，返回新入站的 id。这个端点收 JSON 体。
func (x *XUI) addInbound(payload map[string]any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	endpoint := x.base() + "/panel/api/inbounds/add"
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+x.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := x.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
		Obj     struct {
			ID int `json:"id"`
		} `json:"obj"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return 0, fmt.Errorf("解析响应失败: %s", strings.TrimSpace(string(raw)))
	}
	if !envelope.Success {
		return 0, fmt.Errorf("%s", envelope.Msg)
	}
	return envelope.Obj.ID, nil
}

// attachClients 把模板入站上的客户端挂到新建的入站，实现一套凭据走多个出口。
func (x *XUI) attachClients(emails []string, inboundID int) error {
	for _, email := range emails {
		err := x.attachOnce(email, inboundID)
		if err == nil {
			continue
		}
		// 面板在 inbounds/add 里内联建的客户端会同步进 clients 表，
		// 却不写 client_inbounds 关联。attach 看到 email 已存在但查不到关联，
		// 就当成新建去插入，撞上 clients.email 的唯一索引报 Duplicate。
		// 先调一次 update 让面板把这条记录补全，再 attach 就能成功。
		if !isDuplicateEmail(err) {
			return err
		}
		if nerr := x.normalizeClient(email); nerr != nil {
			return fmt.Errorf("挂载客户端 %s 失败: %w", email, err)
		}
		if err := x.attachOnce(email, inboundID); err != nil {
			return err
		}
	}
	return nil
}

// attachOnce 调一次面板的 attach 接口。
func (x *XUI) attachOnce(email string, inboundID int) error {
	body, err := json.Marshal(map[string]any{"inboundIds": []int{inboundID}})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/panel/api/clients/%s/attach", x.base(), url.PathEscape(email))
	raw, err := x.jsonRequest(http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}

	var envelope struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("解析 attach 响应失败: %s", strings.TrimSpace(string(raw)))
	}
	if !envelope.Success {
		return fmt.Errorf("挂载客户端 %s 失败: %s", email, envelope.Msg)
	}
	return nil
}

// normalizeClient 用一次空更新让面板把老格式的客户端记录补全。
func (x *XUI) normalizeClient(email string) error {
	body, err := json.Marshal(map[string]any{"email": email})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/panel/api/clients/update/%s", x.base(), url.PathEscape(email))
	raw, err := x.jsonRequest(http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	var envelope struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("解析 update 响应失败: %s", strings.TrimSpace(string(raw)))
	}
	if !envelope.Success {
		return fmt.Errorf("%s", envelope.Msg)
	}
	return nil
}

// jsonRequest 发一个带 JSON 体的请求并读回响应。
func (x *XUI) jsonRequest(method, endpoint string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(method, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+x.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := x.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func isDuplicateEmail(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate email")
}

// clientEmails 取出模板入站上所有客户端的 email，用于 attach。
func clientEmails(tpl map[string]any) ([]string, error) {
	settings, err := asObject(tpl["settings"])
	if err != nil {
		return nil, fmt.Errorf("解析模板 settings 失败: %w", err)
	}
	clients, _ := settings["clients"].([]any)
	out := []string{}
	for _, c := range clients {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if email, _ := cm["email"].(string); email != "" {
			out = append(out, email)
		}
	}
	return out, nil
}

// InboundDetail 是某个入站的完整信息，用于在 fanout 里直接查看而不必跳到面板。
type InboundDetail struct {
	Inbound
	Clients []ClientInfo `json:"clients"`
	Links   []string     `json:"links"`
	Listen  string       `json:"listen"`
	Network string       `json:"network"`
	TLS     string       `json:"tls"`
}

type ClientInfo struct {
	BoundTo string `json:"bound_to,omitempty"`
	Email   string `json:"email"`
	ID      string `json:"id"`
	Enable  bool   `json:"enable"`
}

// InboundDetail 取一个入站的详情，含客户端与分享链接。
// 分享链接里面板会写 localhost，这里换成实际可连的地址。
func (x *XUI) InboundDetail(id int, publicHost string) (*InboundDetail, error) {
	raw, err := x.rawInbound(id)
	if err != nil {
		return nil, err
	}

	settings, err := asObject(raw["settings"])
	if err != nil {
		return nil, fmt.Errorf("解析 settings 失败: %w", err)
	}
	stream, _ := asObject(raw["streamSettings"])
	if stream["security"] == "tls" {
		settings, _ := asObject(stream["tlsSettings"])
		name, _ := settings["serverName"].(string)
		publicHost = publishedTLSHost(name, publicHost)
	}

	bound, err := x.boundInbounds()
	if err != nil {
		return nil, err
	}

	streamJSON, _ := json.Marshal(raw["streamSettings"])
	port := int(toFloat(raw["port"]))
	apiTag, _ := raw["tag"].(string)
	tag := resolvedInboundTag(apiTag, port, streamJSON)

	detail := &InboundDetail{
		Inbound: Inbound{
			ID:       id,
			Port:     port,
			Protocol: fmt.Sprint(raw["protocol"]),
			Remark:   fmt.Sprint(raw["remark"]),
			Enable:   raw["enable"] == true,
			Tag:      tag,
			BoundTo:  bound[tag],
		},
		Listen: fmt.Sprint(orEmpty(raw["listen"])),
	}
	if stream != nil {
		detail.Network = fmt.Sprint(orEmpty(stream["network"]))
		detail.TLS = fmt.Sprint(orEmpty(stream["security"]))
	}

	clients, _ := settings["clients"].([]any)
	for _, c := range clients {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		info := ClientInfo{
			Email:  fmt.Sprint(orEmpty(cm["email"])),
			ID:     fmt.Sprint(orEmpty(cm["id"])),
			Enable: cm["enable"] != false,
		}
		detail.Clients = append(detail.Clients, info)

		if links, err := x.clientLinks(info.Email); err == nil {
			for _, l := range links {
				if fixed, ok := linkForPort(l, port, publicHost); ok {
					detail.Links = append(detail.Links, fixed)
				}
			}
		}
	}
	return detail, nil
}

// linkForPort 从一批分享链接里挑出属于指定端口的那条，并把面板写的
// localhost 换成实际可连的地址。
//
// vmess 的链接是 base64 编码的 JSON（vmess://<base64>），端口和地址都在里面，
// 按 URI 形式匹配 ":端口?" 一条也筛不出来，得先解码。
func linkForPort(link string, port int, publicHost string) (string, bool) {
	if strings.HasPrefix(link, "vmess://") {
		return fixVMessLink(link, port, publicHost)
	}
	if strings.Contains(link, fmt.Sprintf(":%d?", port)) || strings.Contains(link, fmt.Sprintf(":%d#", port)) {
		return strings.Replace(link, "@localhost:", "@"+publicHost+":", 1), true
	}
	return "", false
}

// fixVMessLink 解码 vmess 链接，确认端口后把 add 换成实际地址再编码回去。
// 解不开就按原样放行：宁可给一条地址还是 localhost 的链接，也别整条丢掉。
func fixVMessLink(link string, port int, publicHost string) (string, bool) {
	payload := strings.TrimPrefix(link, "vmess://")
	blob, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// 有的面板版本用 URI 形式的 vmess，退回通用匹配
		return "", strings.Contains(link, fmt.Sprintf(":%d?", port)) ||
			strings.Contains(link, fmt.Sprintf(":%d#", port))
	}
	var conf map[string]any
	if err := json.Unmarshal(blob, &conf); err != nil {
		return "", false
	}
	if int(toFloat(conf["port"])) != port {
		return "", false
	}
	if fmt.Sprint(orEmpty(conf["add"])) == "localhost" {
		conf["add"] = publicHost
	}
	fixed, err := json.Marshal(conf)
	if err != nil {
		return link, true
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(fixed), true
}

// clientLinks 取某个客户端在所有入站上的分享链接。
func (x *XUI) clientLinks(email string) ([]string, error) {
	obj, err := x.get("panel/api/clients/links/" + url.PathEscape(email))
	if err != nil {
		return nil, err
	}
	var links []string
	if err := json.Unmarshal(obj, &links); err != nil {
		return nil, err
	}
	return links, nil
}

// InboundLinks 批量取多个入站的分享链接，用于一次性导出。
func (x *XUI) InboundLinks(ids []int, publicHost string) ([]string, error) {
	var out []string
	for _, id := range ids {
		detail, err := x.InboundDetail(id, publicHost)
		if err != nil {
			return out, err
		}
		out = append(out, detail.Links...)
	}
	return out, nil
}

// DeleteInbounds 删除入站，并顺手清掉指向它们的 fanout 路由规则。
// 面板的 del 只动 inbounds，残留的规则会让后续绑定读到不存在的入站标签。
func (x *XUI) DeleteInbounds(ids []int, tunnels []*Tunnel) error {
	for _, id := range ids {
		if _, err := x.post(fmt.Sprintf("panel/api/inbounds/del/%d", id), nil); err != nil {
			return fmt.Errorf("删除入站 %d 失败: %w", id, err)
		}
	}

	setting, testURL, err := x.loadXray()
	if err != nil {
		return err
	}
	x.syncOutbounds(setting, tunnels)

	remain, err := x.Inbounds(nil)
	if err != nil {
		return err
	}
	alive := map[string]bool{}
	for _, ib := range remain {
		alive[ib.Tag] = true
	}

	routing, _ := setting["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
	}
	rules, _ := routing["rules"].([]any)
	cleaned := make([]any, 0, len(rules))
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			cleaned = append(cleaned, r)
			continue
		}
		outTag, _ := m["outboundTag"].(string)
		if !strings.HasPrefix(outTag, xuiTagPrefix) {
			cleaned = append(cleaned, r)
			continue
		}
		kept := []any{}
		for _, it := range toStringSlice(m["inboundTag"]) {
			if alive[it] {
				kept = append(kept, it)
			}
		}
		if len(kept) > 0 {
			m["inboundTag"] = kept
			cleaned = append(cleaned, m)
		}
	}
	routing["rules"] = cleaned
	setting["routing"] = routing
	return x.saveXray(setting, testURL)
}

// Rebind 把原本绑到 oldHost 的入站改绑到新节点上。
// 隧道换节点后出站 tag 会变，需要同步路由规则。
func (x *XUI) Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error {
	list, err := x.Inbounds(nil)
	if err != nil {
		return err
	}
	oldTag := sanitizeTag(oldHost)
	newLabel := exitLabel(target)
	for _, ib := range list {
		if ib.BoundTo != oldTag {
			continue
		}
		if err := x.Bind(ib.Tag, target.Node.HostName, tunnels); err != nil {
			return err
		}
		// 备注里带着旧出口的地区和 IP 尾段，换了节点要跟着改，否则名不副实
		if renamed := renameExitLabel(ib.Remark, newLabel); renamed != ib.Remark {
			if err := x.renameInbound(ib.ID, renamed); err != nil {
				return err
			}
		}
	}
	return nil
}

// renameExitLabel 在换节点之后把别名改成新出口的。
//
// 只改 fanout 自己起的名字（开头是国旗那种）。以前是按 "-" 切段替换末两段，
// 既会把用户自己的备注切坏，又会在反复复制时叠成 "a-JP-243-VN-165" 这种。
// 现在整体替换，用户手工改过的名字一律不碰。
func renameExitLabel(remark, newLabel string) string {
	if newLabel == "" || !isGeneratedLabel(remark) {
		return remark
	}
	return newLabel
}

// postJSON 向面板发一个 JSON 体的请求，并解析它统一的 success/msg 信封。
// 面板的 inbounds/clients 写接口都收 JSON 而不是表单，所以不能走 x.call。
func (x *XUI) postJSON(path string, payload any, what string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/%s", x.base(), strings.TrimPrefix(path, "/"))
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+x.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := x.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	blob, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Success bool   `json:"success"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(blob, &envelope); err != nil {
		return fmt.Errorf("解析%s响应失败: %s", what, strings.TrimSpace(string(blob)))
	}
	if !envelope.Success {
		return fmt.Errorf("%s失败: %s", what, envelope.Msg)
	}
	return nil
}

// inboundPayload 把面板返回的原始入站转成 update 接口要的形状。
// 面板的 update 是整体覆盖，没带上的字段会被清掉，所以要原样回填。
func inboundPayload(raw map[string]any) map[string]any {
	return map[string]any{
		"enable":         raw["enable"],
		"remark":         fmt.Sprint(orEmpty(raw["remark"])),
		"listen":         fmt.Sprint(orEmpty(raw["listen"])),
		"port":           int(toFloat(raw["port"])),
		"protocol":       fmt.Sprint(raw["protocol"]),
		"expiryTime":     0,
		"total":          0,
		"settings":       mustJSONField(raw["settings"]),
		"streamSettings": mustJSONField(raw["streamSettings"]),
		"sniffing":       mustJSONField(raw["sniffing"]),
		"allocate":       mustJSON(map[string]any{}),
	}
}

// updateInboundRaw 读出入站、让 mutate 改 payload，再整体写回。
func (x *XUI) updateInboundRaw(id int, what string, mutate func(payload map[string]any, raw map[string]any) error) error {
	raw, err := x.rawInbound(id)
	if err != nil {
		return err
	}
	payload := inboundPayload(raw)
	if mutate != nil {
		if err := mutate(payload, raw); err != nil {
			return err
		}
	}
	return x.postJSON(fmt.Sprintf("panel/api/inbounds/update/%d", id), payload, what)
}

// renameInbound 只改备注，其余配置原样写回。
func (x *XUI) renameInbound(id int, remark string) error {
	return x.updateInboundRaw(id, "改名", func(p, _ map[string]any) error {
		p["remark"] = remark
		return nil
	})
}

// UpdateInbound 改端口、备注与启停，其余配置原样写回。
//
// 改端口会同时改掉 inboundTag（面板用 in-<端口>-<网络> 命名），
// 所以绑定关系要跟着迁移，否则路由规则会指向一个不存在的入站。
func (x *XUI) UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error {
	if patch.Port != nil {
		used, err := x.usedPorts()
		if err != nil {
			return err
		}
		cur, err := x.rawInbound(id)
		if err != nil {
			return err
		}
		if p := *patch.Port; p != int(toFloat(cur["port"])) && used[p] {
			return fmt.Errorf("端口 %d 已被别的入站占用", p)
		}
	}

	// 改端口前记下旧 tag 与它的绑定，改完再按新 tag 绑回去
	var oldTag, boundTo string
	if patch.Port != nil {
		list, err := x.Inbounds(nil)
		if err != nil {
			return err
		}
		for _, ib := range list {
			if ib.ID == id {
				oldTag, boundTo = ib.Tag, ib.BoundTo
				break
			}
		}
	}

	if err := x.updateInboundRaw(id, "改入站", func(p, _ map[string]any) error {
		if patch.Port != nil {
			p["port"] = *patch.Port
		}
		if patch.Remark != nil {
			p["remark"] = *patch.Remark
		}
		if patch.Enable != nil {
			p["enable"] = *patch.Enable
		}
		return nil
	}); err != nil {
		return err
	}

	if oldTag == "" || boundTo == "" {
		return nil
	}
	// 端口变了 tag 也变了，把绑定迁到新 tag 上
	list, err := x.Inbounds(nil)
	if err != nil {
		return err
	}
	for _, ib := range list {
		if ib.ID != id || ib.Tag == oldTag {
			continue
		}
		var host string
		for _, t := range tunnels {
			if sanitizeTag(t.Node.HostName) == boundTo {
				host = t.Node.HostName
				break
			}
		}
		if host == "" {
			return nil
		}
		return x.Bind(ib.Tag, host, tunnels)
	}
	return nil
}

// AddClient 给入站加一个客户端。
func (x *XUI) AddClient(id int, email string, tunnels []*Tunnel) error {
	raw, err := x.rawInbound(id)
	if err != nil {
		return err
	}
	proto := fmt.Sprint(raw["protocol"])
	if email == "" {
		email = fmt.Sprintf("%s-%d-%s", proto, int(toFloat(raw["port"])), randomHex(3))
	}
	return x.updateInboundRaw(id, "加客户端", func(p, raw map[string]any) error {
		settings, err := asObject(raw["settings"])
		if err != nil {
			return fmt.Errorf("解析 settings 失败: %w", err)
		}
		clients, _ := settings["clients"].([]any)
		for _, c := range clients {
			if cm, ok := c.(map[string]any); ok && fmt.Sprint(orEmpty(cm["email"])) == email {
				return fmt.Errorf("客户端 %s 已存在", email)
			}
		}
		settings["clients"] = append(clients, newClientEntry(proto, email))
		p["settings"] = mustJSON(settings)
		return nil
	})
}

// DeleteClient 摘掉入站上的一个客户端。
func (x *XUI) DeleteClient(id int, email string, tunnels []*Tunnel) error {
	return x.updateInboundRaw(id, "删客户端", func(p, raw map[string]any) error {
		settings, err := asObject(raw["settings"])
		if err != nil {
			return fmt.Errorf("解析 settings 失败: %w", err)
		}
		clients, _ := settings["clients"].([]any)
		kept := make([]any, 0, len(clients))
		for _, c := range clients {
			if cm, ok := c.(map[string]any); ok && fmt.Sprint(orEmpty(cm["email"])) == email {
				continue
			}
			kept = append(kept, c)
		}
		if len(kept) == len(clients) {
			return fmt.Errorf("客户端 %s 不存在", email)
		}
		if len(kept) == 0 {
			return fmt.Errorf("这是最后一个客户端，删掉入站就没人能连了")
		}
		settings["clients"] = kept
		p["settings"] = mustJSON(settings)
		return nil
	})
}

// ResetClient 换掉客户端凭据，已分发的旧链接随即失效。
func (x *XUI) ResetClient(id int, email string, tunnels []*Tunnel) error {
	return x.updateInboundRaw(id, "重置凭据", func(p, raw map[string]any) error {
		proto := fmt.Sprint(raw["protocol"])
		settings, err := asObject(raw["settings"])
		if err != nil {
			return fmt.Errorf("解析 settings 失败: %w", err)
		}
		clients, _ := settings["clients"].([]any)
		found := false
		for _, c := range clients {
			cm, ok := c.(map[string]any)
			if !ok || fmt.Sprint(orEmpty(cm["email"])) != email {
				continue
			}
			found = true
			if proto == "trojan" {
				cm["password"] = randomHex(8)
			} else {
				cm["id"] = newUUID()
			}
		}
		if !found {
			return fmt.Errorf("客户端 %s 不存在", email)
		}
		settings["clients"] = clients
		p["settings"] = mustJSON(settings)
		return nil
	})
}

// newClientEntry 按协议造一个新客户端条目。
func newClientEntry(proto, email string) map[string]any {
	// 字段与面板自己生成的客户端保持一致：tgId 是数字（给字符串面板会直接报
	// json unmarshal 错），subId 留空由面板按需分配。
	c := map[string]any{
		"email": email, "enable": true, "comment": "", "security": "",
		"expiryTime": 0, "totalGB": 0, "limitIp": 0,
		"tgId": 0, "subId": "", "reset": 0,
	}
	if proto == "trojan" {
		c["password"] = randomHex(8)
	} else {
		c["id"] = newUUID()
	}
	if proto == "vless" {
		c["flow"] = ""
	}
	return c
}

// mustJSONField 兼容字段是对象或已编码字符串两种情况，统一输出字符串。
func mustJSONField(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return mustJSON(v)
}

// isAllDigits 判断 tag 后缀是不是纯数字（早期用槽位号命名出站留下的遗留格式）。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// OnTunnelsChanged 对 3x-ui 是空操作：出站在 Bind/CloneToTunnels 里已经顺带
// 同步过，这里再写一次只会多重启一遍面板的 Xray，把已有连接打断。
func (x *XUI) OnTunnelsChanged(tunnels []*Tunnel) error { return nil }

// Close 对 3x-ui 是空操作：Xray 由面板自己管，不该被 fanout 停掉。
func (x *XUI) Close() {}

// ResyncOutbound 重写某条隧道对应的出站配置。
// 用于隧道原地重连（节点名没变）后刷新端口等信息。
func (x *XUI) ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error {
	setting, testURL, err := x.loadXray()
	if err != nil {
		return err
	}
	x.syncOutbounds(setting, tunnels)
	return x.saveXray(setting, testURL)
}

// forceIPv4 让直连类出站只走 IPv4。
//
// 隧道内没有 IPv6，但没被路由规则匹配上的流量会走 direct 出站直连；
// 母机有全局 IPv6 时这部分会从 IPv6 出去，暴露服务器真实地址。
func forceIPv4(outbound map[string]any) {
	if proto, _ := outbound["protocol"].(string); proto != "freedom" {
		return
	}
	settings, _ := outbound["settings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
		outbound["settings"] = settings
	}
	settings["domainStrategy"] = "UseIPv4"
}

// xuiRunning 判断 3x-ui 服务是否在跑。
//
// Alpine 这类发行版用 OpenRC 而不是 systemd，只查 systemctl 会误判成"没装"，
// 于是装了面板也会退回自建模式，两个 Xray 抢端口。
func xuiRunning() bool {
	if exec.Command("systemctl", "is-active", "--quiet", "x-ui").Run() == nil {
		return true
	}
	if exec.Command("rc-service", "x-ui", "status").Run() == nil {
		return true
	}
	return false
}

// CreateInbound 通过面板的 inbounds/add API 新建一个入站。
//
// 走 API 而不是直接写库：面板会自己维护 tag、客户端关联和分享链接，
// fanout 插手它的 sqlite 只会两边打架。载荷字段照面板自己生成的入站抄，
// settings/streamSettings/sniffing 要编码成字符串，这是面板 API 的要求。
func (x *XUI) CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error) {
	used, err := x.usedPorts()
	if err != nil {
		return nil, err
	}
	ns, err := normalizeInboundSpec(spec, used)
	if err != nil {
		return nil, err
	}

	// 借用自建模式那套入站描述来生成 streamSettings：TLS 自签证书、
	// REALITY 密钥的生成逻辑两种后端完全一样，没必要写第二份。
	ib := &nativeInbound{
		Port:     ns.Port,
		Protocol: ns.Protocol,
		Network:  ns.Network,
		Path:     ns.Path,
		Host:     ns.Host,
		Security: ns.Security,
		Remark:   ns.Remark,
		Enable:   true,
	}
	switch ns.Security {
	case "tls":
		conf, err := buildTLS(x.workDir, spec)
		if err != nil {
			return nil, err
		}
		ib.TLS = conf
	case "reality":
		bin, err := findXray(x.workDir)
		if err != nil {
			return nil, fmt.Errorf("REALITY 需要 xray 生成密钥: %w", err)
		}
		conf, err := buildReality(bin, spec)
		if err != nil {
			return nil, err
		}
		ib.Reality = conf
	}

	client := newClientEntry(ns.Protocol, fmt.Sprintf("%s-%d", ns.Protocol, ns.Port))
	if ns.Protocol == "vless" {
		client["flow"] = ns.Flow
	}
	settings := map[string]any{
		"clients":   []any{client},
		"fallbacks": []any{},
	}
	if ns.Protocol == "vless" {
		settings["decryption"] = "none"
	}

	payload := map[string]any{
		"enable":         true,
		"remark":         ns.Remark,
		"listen":         "",
		"port":           ns.Port,
		"protocol":       ns.Protocol,
		"expiryTime":     0,
		"total":          0,
		"settings":       mustJSON(settings),
		"streamSettings": mustJSON(xuiStreamSettings(ib)),
		"sniffing":       mustJSON(map[string]any{"enabled": true, "destOverride": []any{"http", "tls"}}),
		"allocate":       mustJSON(map[string]any{}),
	}

	id, err := x.addInbound(payload)
	if err != nil {
		return nil, fmt.Errorf("面板新建入站失败: %w", err)
	}
	return &CreatedInbound{
		ID:       id,
		Port:     ns.Port,
		Protocol: ns.Protocol,
		Remark:   ns.Remark,
		Network:  ns.Network,
		Security: ns.Security,
	}, nil
}

// xuiStreamSettings 在自建模式的 streamSettings 基础上补面板要的字段。
//
// 面板生成分享链接时要读 realitySettings.settings 里的 publicKey / fingerprint，
// Xray 自己不用这些，但缺了面板给出的链接客户端连不上。
func xuiStreamSettings(ib *nativeInbound) map[string]any {
	stream := streamSettingsJSON(ib)
	if ib.securityOrNone() == "reality" && ib.Reality != nil {
		r, _ := stream["realitySettings"].(map[string]any)
		if r != nil {
			r["show"] = false
			r["xver"] = 0
			r["settings"] = map[string]any{
				"publicKey":   ib.Reality.PublicKey,
				"fingerprint": ib.Reality.Fingerprint,
				"spiderX":     "/",
			}
		}
	}
	return stream
}
