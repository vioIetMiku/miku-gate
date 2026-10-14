package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// XCL 对接 byJoey/xray-cf-lite 装出来的系统级 Xray。
//
// xray-cf-lite 把 Xray 装成系统服务，配置固定落在 /usr/local/etc/xray/config.json，
// 生成若干个 ws 入站并套 Cloudflare 前置，但出站只有 direct/block、没有分流。
// fanout 在这个后端下不新建/删除节点，只负责给这些入站加“走哪条家宽出口”的路由：
// 往同一份 config 注入 miku- 前缀的 socks 出站和 routing 规则，
// 只碰自己前缀的条目，xray-cf-lite 管的 inbounds 原样保留，两边互不覆盖。
type XCL struct {
	cfgPath string
	initSys string // "systemd" 或 "openrc"
	svcName string // xray-cf-lite 的服务名，固定 "xray"
	// restart 单独拎出来，测试里可以换成不真的去动系统服务
	restart func() error
}

const (
	xclConfigPath = "/usr/local/etc/xray/config.json"
	xclStateDir   = "/etc/xray-cf-lite"
)

// DetectXCL 判断本机是否由 xray-cf-lite 接管。
//
// 条件：存在 xray-cf-lite 的状态目录，且系统 Xray 配置可读。
// 只有两者都满足才认，避免把手工装的 Xray 也当成 xray-cf-lite。
func DetectXCL() (*XCL, error) {
	if _, err := os.Stat(xclStateDir); err != nil {
		return nil, fmt.Errorf("未发现 xray-cf-lite 状态目录 %s", xclStateDir)
	}
	if _, err := os.Stat(xclConfigPath); err != nil {
		return nil, fmt.Errorf("未发现 Xray 配置 %s", xclConfigPath)
	}
	x := &XCL{cfgPath: xclConfigPath, svcName: "xray"}
	x.initSys = detectInitSystem()
	x.restart = x.restartXray
	// 试读一次，坏配置早点暴露
	if _, err := x.loadCfg(); err != nil {
		return nil, err
	}
	return x, nil
}

// detectInitSystem 判定当前是 systemd 还是 OpenRC，与 xray-cf-lite 的探测保持一致。
func detectInitSystem() string {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return "systemd"
	}
	return "openrc"
}

func (x *XCL) Kind() string { return "xray-cf-lite" }

func (x *XCL) Describe() string {
	return fmt.Sprintf("xray-cf-lite 接管（%s，配置 %s）", x.initSys, x.cfgPath)
}

// loadCfg 读取并解析 xray-cf-lite 的 config.json。
func (x *XCL) loadCfg() (map[string]any, error) {
	blob, err := os.ReadFile(x.cfgPath)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", x.cfgPath, err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", x.cfgPath, err)
	}
	return cfg, nil
}

// saveCfg 写回配置并重启系统 Xray。
//
// 先写临时文件再原子改名，避免半截配置让 Xray 起不来。
func (x *XCL) saveCfg(cfg map[string]any) error {
	blob, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	tmp := x.cfgPath + ".fanout.tmp"
	if err := os.WriteFile(tmp, blob, 0644); err != nil {
		return fmt.Errorf("写入临时配置失败: %w", err)
	}
	if err := os.Rename(tmp, x.cfgPath); err != nil {
		return fmt.Errorf("替换配置失败: %w", err)
	}
	if x.restart == nil {
		return x.restartXray()
	}
	return x.restart()
}

func (x *XCL) restartXray() error {
	var cmd *exec.Cmd
	if x.initSys == "systemd" {
		cmd = exec.Command("systemctl", "restart", x.svcName)
	} else {
		cmd = exec.Command("rc-service", x.svcName, "restart")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("重启 %s 失败: %s", x.svcName, trimOutput(out))
	}
	return nil
}

// xclInboundTag 取 config 里某个 inbound 的 tag，没有就按端口+协议兜底。
func xclInboundTag(m map[string]any) string {
	if tag, _ := m["tag"].(string); tag != "" {
		return tag
	}
	proto, _ := m["protocol"].(string)
	port := jsonInt(m["port"])
	return fmt.Sprintf("in-%s-%d", proto, port)
}

func jsonInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return int(n)
		}
	case int:
		return t
	}
	return 0
}

// Inbounds 列出 xray-cf-lite 生成的入站及其当前绑定的出口。
func (x *XCL) Inbounds(live map[string]bool) ([]Inbound, error) {
	cfg, err := x.loadCfg()
	if err != nil {
		return nil, err
	}
	bound := x.boundInbounds(cfg)

	rawIns, _ := cfg["inbounds"].([]any)
	out := make([]Inbound, 0, len(rawIns))
	for _, raw := range rawIns {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tag := xclInboundTag(m)
		proto, _ := m["protocol"].(string)
		port := jsonInt(m["port"])
		out = append(out, Inbound{
			Stack:    inboundStackJSON(proto, json.RawMessage(mustJSON(m["streamSettings"])), json.RawMessage(mustJSON(m["settings"]))),
			ID:       port, // xray-cf-lite 没有数值 ID，用端口做稳定标识
			Port:     port,
			Protocol: proto,
			Remark:   tag,
			Enable:   true,
			Tag:      tag,
			BoundTo:  bound[tag],
			BoundUp:  live[bound[tag]],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// boundInbounds 从 routing 规则里还原 inboundTag -> 节点主机名 的绑定。
func (x *XCL) boundInbounds(cfg map[string]any) map[string]string {
	bound := map[string]string{}
	routing, _ := cfg["routing"].(map[string]any)
	if routing == nil {
		return bound
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
		if isAllDigits(host) {
			continue
		}
		for _, it := range toStringSlice(m["inboundTag"]) {
			bound[it] = host
		}
	}
	return bound
}

// syncOutbounds 让 config 里 miku- 出站与当前连通的隧道一致，保留 xray-cf-lite 的 direct/block。
func (x *XCL) syncOutbounds(cfg map[string]any, tunnels []*Tunnel) {
	outbounds, _ := cfg["outbounds"].([]any)
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
	cfg["outbounds"] = kept
}

// Bind 把某个入站的流量导向指定隧道。hostname 留空表示解绑，恢复走 xray-cf-lite 的 direct。
func (x *XCL) Bind(inboundTag string, hostname string, tunnels []*Tunnel) error {
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
	if !knownTags[inboundTag] {
		return fmt.Errorf("入站 %s 不存在", inboundTag)
	}

	cfg, err := x.loadCfg()
	if err != nil {
		return err
	}
	x.syncOutbounds(cfg, tunnels)

	routing, _ := cfg["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
	}
	rules, _ := routing["rules"].([]any)

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
	cfg["routing"] = routing
	return x.saveCfg(cfg)
}

// Rebind 把绑在 oldHost 上的入站整体改绑到 target，用于换节点时批量迁移。
func (x *XCL) Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error {
	if target == nil {
		return fmt.Errorf("目标节点为空")
	}
	cfg, err := x.loadCfg()
	if err != nil {
		return err
	}
	bound := x.boundInbounds(cfg)
	moved := 0
	for tag, host := range bound {
		if host == oldHost {
			if err := x.Bind(tag, target.Node.HostName, tunnels); err != nil {
				return err
			}
			moved++
		}
	}
	if moved == 0 {
		return fmt.Errorf("没有入站绑在 %s 上", oldHost)
	}
	return nil
}

// ResyncOutbound 隧道集合变化后重刷出站，保证 miku- 出站与实际隧道一致。
func (x *XCL) ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error {
	return x.OnTunnelsChanged(tunnels)
}

// OnTunnelsChanged 在隧道集合变化后重写 miku- 出站并重启 Xray。
func (x *XCL) OnTunnelsChanged(tunnels []*Tunnel) error {
	cfg, err := x.loadCfg()
	if err != nil {
		return err
	}
	x.syncOutbounds(cfg, tunnels)
	return x.saveCfg(cfg)
}

// InboundDetail 返回单个入站的详情（含订阅链接由上层用 xray-cf-lite 自己的订阅体系给）。
func (x *XCL) InboundDetail(id int, publicHost string) (*InboundDetail, error) {
	ins, err := x.Inbounds(map[string]bool{})
	if err != nil {
		return nil, err
	}
	for _, ib := range ins {
		if ib.ID == id {
			return &InboundDetail{Inbound: ib}, nil
		}
	}
	return nil, fmt.Errorf("入站 %d 不存在", id)
}

// InboundLinks 在这个后端下不生成链接：节点链接由 xray-cf-lite 的订阅服务负责。
func (x *XCL) InboundLinks(ids []int, publicHost string) ([]string, error) {
	return nil, nil
}

// ---- 以下操作在 xray-cf-lite 模式下禁用：节点由 xray-cf-lite 管，fanout 只改路由 ----

var errXCLReadOnly = fmt.Errorf("xray-cf-lite 模式下节点由 xray-cf-lite 管理，fanout 只能改路由（绑定出口），不能增删或改节点本身")

func (x *XCL) CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error) {
	return nil, errXCLReadOnly
}

func (x *XCL) CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error) {
	return nil, errXCLReadOnly
}

func (x *XCL) DeleteInbounds(ids []int, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

func (x *XCL) UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

func (x *XCL) AddClient(id int, email string, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

func (x *XCL) DeleteClient(id int, email string, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

func (x *XCL) ResetClient(id int, email string, tunnels []*Tunnel) error {
	return errXCLReadOnly
}

// Close 无自管进程，空实现。
func (x *XCL) Close() {}
