package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Panel 是 fanout 管理节点链接的后端。
//
// 有两个实现：接管本机 3x-ui 面板的 XUI，以及 fanout 自己跑 Xray 的 Native。
// 界面和编排层只依赖这个接口，两种模式下的操作语义完全一致。
type Panel interface {
	// Kind 返回 "3x-ui" 或 "native"，界面据此提示当前模式。
	Kind() string
	// Describe 给出一行人能读的后端说明。
	Describe() string

	Inbounds(live map[string]bool) ([]Inbound, error)
	InboundDetail(id int, publicHost string) (*InboundDetail, error)
	InboundLinks(ids []int, publicHost string) ([]string, error)

	Bind(inboundTag string, hostname string, tunnels []*Tunnel) error
	Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error
	ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error

	CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error)
	DeleteInbounds(ids []int, tunnels []*Tunnel) error

	// CreateInbound 新建一个入站。自建模式写自己的库并重建 Xray 配置，
	// 接管 3x-ui 时走面板的 inbounds/add API，让面板照常管这条入站。
	CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error)

	// UpdateInbound 改端口、备注与启停。只有非零/非 nil 的字段会被写入。
	UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error

	// AddClient 给入站加一个客户端，email 留空时自动命名。
	AddClient(id int, email string, tunnels []*Tunnel) error
	// DeleteClient 摘掉入站上的一个客户端。
	DeleteClient(id int, email string, tunnels []*Tunnel) error
	// ResetClient 换掉客户端的凭据（UUID / trojan 密码），已分发的旧链接随即失效。
	ResetClient(id int, email string, tunnels []*Tunnel) error

	// OnTunnelsChanged 在隧道集合变化后调用。
	//
	// 自建模式的出站完全由隧道列表推导，新开的出口必须重建配置才有对应出站；
	// 接管 3x-ui 时出站在 Bind/Clone 里顺带同步，这里是空操作，
	// 免得每开一条隧道就白重启一次面板的 Xray。
	OnTunnelsChanged(tunnels []*Tunnel) error

	// Close 释放后端占用的资源。自建模式要停掉自己拉起的 Xray，
	// 否则 fanout 退出后它会变成孤儿进程，下次启动撞端口。
	Close()
}

// InboundPatch 描述对入站的一次局部修改。指针为 nil 表示该字段不动。
type InboundPatch struct {
	Port   *int
	Remark *string
	Enable *bool
}

// CreatedInbound 是新建入站后回给界面的摘要。
type CreatedInbound struct {
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Remark   string `json:"remark"`
	Network  string `json:"network"`
	Security string `json:"security"`
}

// closePanel 在进程退出时释放后端资源。
func closePanel() {
	panelState.mu.Lock()
	p := panelState.current
	panelState.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

// panelState 缓存已选定的后端。探测涉及执行 x-ui 命令，没必要每个请求都做一次。
var panelState struct {
	mu      sync.Mutex
	current Panel
	workDir string
	forced  string
}

// panelModeFile 存界面里选过的后端。命令行 -panel 优先级更高。
func panelModeFile(dir string) string { return filepath.Join(dir, "panel_mode") }

// configurePanel 记录自建模式需要的工作目录与用户指定的模式。
// mode 为空表示读盘上界面选过的模式，都没有才自动探测。
func configurePanel(workDir, mode string) {
	panelState.mu.Lock()
	defer panelState.mu.Unlock()
	panelState.workDir = workDir
	if mode == "" {
		blob, err := os.ReadFile(panelModeFile(workDir))
		if err == nil {
			mode = strings.TrimSpace(string(blob))
		}
	}
	panelState.forced = mode
	panelState.current = nil
}

// savePanelMode 把界面选的后端记到工作目录，重启后仍然生效。空值等于删档回到自动探测。
func savePanelMode(dir, mode string) error {
	if dir == "" {
		return nil
	}
	path := panelModeFile(dir)
	if mode == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte(mode), 0600)
}

// openPanel 返回当前可用的后端。
//
// 优先接管本机已装的 3x-ui：用户既然装了面板，入站大概率在那边管着，
// fanout 另起一个 Xray 会和面板抢端口。探测不到才用自建模式。
func openPanel() (Panel, error) {
	panelState.mu.Lock()
	defer panelState.mu.Unlock()

	if panelState.current != nil {
		return panelState.current, nil
	}

	switch panelState.forced {
	case "3x-ui":
		x, err := DetectXUI(panelState.workDir)
		if err != nil {
			return nil, fmt.Errorf("指定了 3x-ui 模式但探测失败: %w", err)
		}
		panelState.current = x
		return x, nil
	case "native":
		n, err := openNative(panelState.workDir)
		if err != nil {
			return nil, err
		}
		panelState.current = n
		return n, nil
	case "sing-box":
		n, err := openNativeCore(panelState.workDir, "sing-box")
		if err != nil {
			return nil, err
		}
		panelState.current = n
		return n, nil
	case "s-ui":
		s, err := DetectSUI(panelState.workDir)
		if err != nil {
			return nil, err
		}
		panelState.current = s
		return s, nil
	case "xray-cf-lite":
		xc, err := DetectXCL()
		if err != nil {
			return nil, fmt.Errorf("指定了 xray-cf-lite 模式但探测失败: %w", err)
		}
		panelState.current = xc
		return xc, nil
	}

	if xc, err := DetectXCL(); err == nil {
		panelState.current = xc
		return xc, nil
	}

	if x, err := DetectXUI(panelState.workDir); err == nil {
		panelState.current = x
		return x, nil
	} else if !xuiAbsent() {
		// 面板装了却读不出配置，这时自建模式会和它抢端口，宁可报错让用户看见
		return nil, fmt.Errorf("检测到 3x-ui 但读取配置失败: %w", err)
	}

	if suiInstalled() {
		s, err := DetectSUI(panelState.workDir)
		if err != nil {
			return nil, err
		}
		panelState.current = s
		return s, nil
	}
	n, err := openNative(panelState.workDir)
	if err != nil {
		return nil, err
	}
	panelState.current = n
	return n, nil
}

// currentPanelMode 返回当前生效的后端类型（forced 为空时按已选定的 current 推断）。
func currentPanelMode() string {
	panelState.mu.Lock()
	defer panelState.mu.Unlock()
	if panelState.forced != "" {
		return panelState.forced
	}
	if panelState.current != nil {
		return panelState.current.Kind()
	}
	return ""
}

// availablePanelModes 探测每种后端在本机是否可用，供界面渲染可选项。
func availablePanelModes(workDir string) []map[string]any {
	modes := []map[string]any{}

	xcOK, xcReason := true, ""
	if _, err := DetectXCL(); err != nil {
		xcOK, xcReason = false, err.Error()
	}
	modes = append(modes, map[string]any{"mode": "xray-cf-lite", "label": "xray-cf-lite", "available": xcOK, "reason": xcReason})

	xuiOK, xuiReason := true, ""
	if _, err := DetectXUI(workDir); err != nil {
		xuiOK, xuiReason = false, err.Error()
	}
	modes = append(modes, map[string]any{"mode": "3x-ui", "label": "3x-ui 面板", "available": xuiOK, "reason": xuiReason})
	modes = append(modes, map[string]any{"mode": "s-ui", "label": "S-UI 面板", "available": suiInstalled(), "reason": "需要本机S-UI；选择时自动读取本机API令牌"})

	// 自建模式总是可用（fanout 自己跑 Xray），前提是能找到 xray 二进制，这里不预判，交给切换时报错。
	modes = append(modes, map[string]any{"mode": "native", "label": "自建 Xray", "available": true, "reason": ""})
	_, singErr := findSingBox(workDir)
	singReason := ""
	if singErr != nil {
		singReason = singErr.Error()
	}
	modes = append(modes, map[string]any{"mode": "sing-box", "label": "自建 sing-box", "available": singErr == nil, "reason": singReason})

	return modes
}

// switchPanelMode 运行时切换后端。mode 传空表示恢复自动探测。
//
// 先关掉旧后端释放资源，再按新模式探测；探测失败时回滚到自动模式，
// 避免把 fanout 卡在一个连不上的后端上。
func switchPanelMode(mode string) (Panel, error) {
	switch mode {
	case "", "3x-ui", "s-ui", "native", "sing-box", "xray-cf-lite":
	default:
		return nil, fmt.Errorf("未知后端模式 %q", mode)
	}

	panelState.mu.Lock()
	old := panelState.current
	workDir := panelState.workDir
	panelState.mu.Unlock()
	if mode == "sing-box" {
		store, err := loadNativeStore(workDir)
		if err != nil {
			return nil, err
		}
		if _, err = buildSingBoxConfig(store.sorted(), nil); err != nil {
			return nil, err
		}
	}
	if old != nil {
		old.Close()
	}

	panelState.mu.Lock()
	panelState.forced = mode
	panelState.current = nil
	panelState.mu.Unlock()

	p, err := openPanel()
	if err != nil {
		// 回滚到自动探测，别把用户卡在坏模式里
		panelState.mu.Lock()
		panelState.forced = ""
		panelState.current = nil
		panelState.mu.Unlock()
		return nil, err
	}
	if err := savePanelMode(workDir, mode); err != nil {
		log.Printf("记录后端模式失败（本次切换仍然生效）: %v", err)
	}
	return p, nil
}

// xuiAbsent 判断本机是否根本没装 3x-ui。
func xuiAbsent() bool {
	if _, err := os.Stat(xuiBinary); err == nil {
		return false
	}
	if _, err := os.Stat(xuiMenu); err == nil {
		return false
	}
	return true
}
