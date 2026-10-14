package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// xclFixture 造一份 xray-cf-lite 风格的 config：三个 ws 入站，出站只有 direct/block、没有分流。
func xclFixture(t *testing.T) *XCL {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := `{
	  "inbounds": [
	    {"tag": "vless-ws", "port": 10001, "protocol": "vless"},
	    {"tag": "trojan-ws", "port": 10002, "protocol": "trojan"},
	    {"tag": "vmess-ws", "port": 10003, "protocol": "vmess"}
	  ],
	  "outbounds": [
	    {"tag": "direct", "protocol": "freedom"},
	    {"tag": "block", "protocol": "blackhole"}
	  ],
	  "routing": {
	    "domainStrategy": "AsIs",
	    "rules": [
	      {"type": "field", "outboundTag": "block", "protocol": ["bittorrent"]}
	    ]
	  }
	}`
	if err := os.WriteFile(path, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	return &XCL{cfgPath: path, initSys: "systemd", svcName: "xray", restart: func() error { return nil }}
}

func xclTunnel(host string, slot, port int) *Tunnel {
	return &Tunnel{Slot: slot, Port: port, Status: "up", Node: Node{HostName: host}}
}

func readCfg(t *testing.T, x *XCL) map[string]any {
	t.Helper()
	cfg, err := x.loadCfg()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func outboundTags(cfg map[string]any) []string {
	var tags []string
	obs, _ := cfg["outbounds"].([]any)
	for _, ob := range obs {
		m, _ := ob.(map[string]any)
		if m == nil {
			continue
		}
		tag, _ := m["tag"].(string)
		tags = append(tags, tag)
	}
	return tags
}

func TestXCLInboundsListsAllThree(t *testing.T) {
	x := xclFixture(t)
	ins, err := x.Inbounds(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 3 {
		t.Fatalf("应列出三个入站，实际 %d", len(ins))
	}
	if ins[0].Port != 10001 || ins[0].Tag != "vless-ws" {
		t.Errorf("入站按端口排序且带 tag，实际 %+v", ins[0])
	}
	for _, ib := range ins {
		if ib.BoundTo != "" {
			t.Errorf("初始状态不该有绑定: %+v", ib)
		}
	}
}

func TestXCLBindAddsRoutingKeepsXCLOutbounds(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}

	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}

	cfg := readCfg(t, x)
	tags := outboundTags(cfg)
	if len(tags) != 3 || tags[0] != "direct" || tags[1] != "block" || tags[2] != "miku-jp-01" {
		t.Fatalf("xray-cf-lite 自己的出站要原样保留、fanout 出站追加在后: %v", tags)
	}

	bound := x.boundInbounds(cfg)
	if bound["vless-ws"] != "jp-01" {
		t.Fatalf("vless-ws 应绑到 jp-01，实际 %v", bound)
	}
	if len(bound) != 1 {
		t.Errorf("只该绑一个入站，实际 %v", bound)
	}

	// 另外两个入站没有规则，仍然走 xray-cf-lite 的默认直连
	ins, err := x.Inbounds(map[string]bool{"jp-01": true})
	if err != nil {
		t.Fatal(err)
	}
	for _, ib := range ins {
		if ib.Tag == "vless-ws" {
			if ib.BoundTo != "jp-01" || !ib.BoundUp {
				t.Errorf("绑定状态应回读出来: %+v", ib)
			}
			continue
		}
		if ib.BoundTo != "" {
			t.Errorf("%s 不该被牵连: %+v", ib.Tag, ib)
		}
	}
}

func TestXCLBindEachInboundToDifferentExit(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{
		xclTunnel("jp-01", 1, 20001),
		xclTunnel("us-02", 2, 20002),
	}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	if err := x.Bind("trojan-ws", "us-02", tunnels); err != nil {
		t.Fatal(err)
	}

	bound := x.boundInbounds(readCfg(t, x))
	if bound["vless-ws"] != "jp-01" || bound["trojan-ws"] != "us-02" {
		t.Fatalf("两个入站应分别绑到不同出口，实际 %v", bound)
	}
}

func TestXCLBindRebindMovesInsteadOfDuplicating(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{
		xclTunnel("jp-01", 1, 20001),
		xclTunnel("us-02", 2, 20002),
	}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	if err := x.Bind("vless-ws", "us-02", tunnels); err != nil {
		t.Fatal(err)
	}

	cfg := readCfg(t, x)
	routing, _ := cfg["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	// xray-cf-lite 自带的 bittorrent 拦截规则要原样留着，加上 fanout 的一条共两条
	if len(rules) != 2 {
		blob, _ := json.Marshal(rules)
		t.Fatalf("换绑应改写而不是叠加规则: %s", blob)
	}
	if x.boundInbounds(cfg)["vless-ws"] != "us-02" {
		t.Errorf("换绑后应指向 us-02")
	}
}

func TestXCLBindEmptyHostUnbinds(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	if err := x.Bind("vless-ws", "", tunnels); err != nil {
		t.Fatal(err)
	}
	if bound := x.boundInbounds(readCfg(t, x)); len(bound) != 0 {
		t.Fatalf("解绑后不该留规则: %v", bound)
	}
}

// xray-cf-lite 自己的 routing 规则不能被 fanout 的绑定/解绑动作误伤
func TestXCLKeepsForeignRoutingRules(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	if err := x.Bind("vless-ws", "", tunnels); err != nil {
		t.Fatal(err)
	}

	cfg := readCfg(t, x)
	routing, _ := cfg["routing"].(map[string]any)
	if routing["domainStrategy"] != "AsIs" {
		t.Errorf("domainStrategy 不该被动: %v", routing["domainStrategy"])
	}
	rules, _ := routing["rules"].([]any)
	if len(rules) != 1 {
		blob, _ := json.Marshal(rules)
		t.Fatalf("应只剩 xray-cf-lite 自己的 bittorrent 规则: %s", blob)
	}
	m, _ := rules[0].(map[string]any)
	if m["outboundTag"] != "block" {
		t.Errorf("留下的应是 bittorrent 拦截规则: %v", m)
	}
}

func TestXCLBindRejectsUnknownInbound(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}
	if err := x.Bind("不存在的-tag", "jp-01", tunnels); err == nil {
		t.Fatal("绑不存在的入站应报错")
	}
}

func TestXCLOnTunnelsChangedDropsDeadOutbounds(t *testing.T) {
	x := xclFixture(t)
	tunnels := []*Tunnel{xclTunnel("jp-01", 1, 20001)}
	if err := x.Bind("vless-ws", "jp-01", tunnels); err != nil {
		t.Fatal(err)
	}
	// 隧道没了，fanout 出站要跟着消失，xray-cf-lite 的 direct/block 留着
	if err := x.OnTunnelsChanged(nil); err != nil {
		t.Fatal(err)
	}
	tags := outboundTags(readCfg(t, x))
	if len(tags) != 2 || tags[0] != "direct" || tags[1] != "block" {
		t.Fatalf("只该剩 xray-cf-lite 自己的出站: %v", tags)
	}
}

func TestXCLNodeOpsAreReadOnly(t *testing.T) {
	x := xclFixture(t)
	if _, err := x.CreateInbound(NewInboundSpec{}, nil); err == nil {
		t.Error("新建节点应被拒绝")
	}
	if err := x.DeleteInbounds([]int{10001}, nil); err == nil {
		t.Error("删除节点应被拒绝")
	}
	if err := x.UpdateInbound(10001, InboundPatch{}, nil); err == nil {
		t.Error("改节点应被拒绝")
	}
}

func TestConfigurePanelReadsSavedMode(t *testing.T) {
	dir := t.TempDir()
	if err := savePanelMode(dir, "xray-cf-lite"); err != nil {
		t.Fatal(err)
	}

	// 命令行没指定时，应该沿用界面上次选的后端
	configurePanel(dir, "")
	if got := currentPanelMode(); got != "xray-cf-lite" {
		t.Fatalf("重启后应恢复界面选过的模式，实际 %q", got)
	}

	// 命令行显式指定时优先级更高
	configurePanel(dir, "native")
	if got := currentPanelMode(); got != "native" {
		t.Fatalf("-panel 应压过盘上的记录，实际 %q", got)
	}

	// 空值等于删档回到自动探测
	if err := savePanelMode(dir, ""); err != nil {
		t.Fatal(err)
	}
	configurePanel(dir, "")
	if got := currentPanelMode(); got != "" {
		t.Fatalf("清空后应回到自动探测，实际 %q", got)
	}
}
