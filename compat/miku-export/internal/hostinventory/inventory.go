// Package hostinventory collects bounded, read-only installation evidence.
// It never opens panel databases, core configurations, private keys or process
// command lines, and never executes a discovered panel/core binary.
package hostinventory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Evidence struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	Version       string `json:"version,omitempty"`
	VersionSource string `json:"version_source,omitempty"`
}
type Service struct {
	Unit           string     `json:"unit"`
	State          string     `json:"state"`
	Active         string     `json:"active"`
	MainPID        int        `json:"main_pid,omitempty"`
	ProcessState   string     `json:"process_state"`
	Executable     string     `json:"executable,omitempty"`
	ListenerState  string     `json:"listener_state"`
	Listeners      []Listener `json:"listeners"`
	ChildrenState  string     `json:"children_state"`
	ChildProcesses []Process  `json:"child_processes"`
}
type Component struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	State        string     `json:"state"`
	Version      string     `json:"version"`
	VersionState string     `json:"version_state"`
	Services     []Service  `json:"services"`
	Evidence     []Evidence `json:"evidence"`
	ReuseReady   bool       `json:"reuse_ready"`
}
type Dependency struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
	Package string `json:"package_hint"`
	State   string `json:"state"`
}
type Report struct {
	CheckedAt           time.Time    `json:"checked_at"`
	Platform            string       `json:"platform"`
	Supported           bool         `json:"supported"`
	ReadOnly            bool         `json:"read_only"`
	Privileged          bool         `json:"privileged"`
	Distribution        string       `json:"distribution"`
	DistributionVersion string       `json:"distribution_version"`
	TUN                 string       `json:"tun"`
	Systemd             string       `json:"systemd"`
	Dependencies        []Dependency `json:"dependencies"`
	Components          []Component  `json:"components"`
	Warnings            []string     `json:"warnings"`
}
type specification struct {
	id, name string
	units    []string
	files    []Evidence
}

var specifications = []specification{
	{"s-ui", "S-UI / sing-box 面板", []string{"s-ui.service"}, []Evidence{{Path: "/usr/local/s-ui/s-ui", Kind: "binary"}, {Path: "/usr/local/s-ui/bin/sing-box", Kind: "embedded_core"}, {Path: "/usr/local/s-ui/db/s-ui.db", Kind: "panel_database"}, {Path: "/usr/local/s-ui/s-ui.db", Kind: "panel_database"}, {Path: "/usr/local/s-ui/bin/config.json", Kind: "config"}}},
	{"x-ui", "3x-ui / x-ui（同路径家族，尚未识别分支）", []string{"x-ui.service", "3x-ui.service"}, []Evidence{{Path: "/usr/local/x-ui/x-ui", Kind: "binary"}, {Path: "/usr/local/x-ui/bin/xray-linux-amd64", Kind: "embedded_core"}, {Path: "/usr/local/x-ui/bin/xray-linux-arm64", Kind: "embedded_core"}, {Path: "/etc/x-ui/x-ui.db", Kind: "panel_database"}, {Path: "/usr/local/x-ui/bin/config.json", Kind: "config"}}},
	{"xray", "独立 Xray", []string{"xray.service"}, []Evidence{{Path: "/usr/local/bin/xray", Kind: "binary"}, {Path: "/usr/bin/xray", Kind: "binary"}, {Path: "/usr/local/etc/xray/config.json", Kind: "config"}, {Path: "/etc/xray/config.json", Kind: "config"}}},
	{"sing-box", "sing-box", []string{"sing-box.service"}, []Evidence{{Path: "/usr/bin/sing-box", Kind: "binary"}, {Path: "/usr/local/bin/sing-box", Kind: "binary"}, {Path: "/etc/sing-box/config.json", Kind: "config"}}},
	{"nginx", "Nginx", []string{"nginx.service"}, []Evidence{{Path: "/usr/sbin/nginx", Kind: "binary"}, {Path: "/etc/nginx/nginx.conf", Kind: "config"}}},
	{"caddy", "Caddy", []string{"caddy.service"}, []Evidence{{Path: "/usr/bin/caddy", Kind: "binary"}, {Path: "/usr/local/bin/caddy", Kind: "binary"}, {Path: "/etc/caddy/Caddyfile", Kind: "config"}}},
}
var dependencies = []Dependency{
	{Name: "openvpn", Purpose: "VPN Gate OpenVPN 出口", Package: "openvpn"},
	{Name: "ip", Purpose: "隔离网络接口与路由", Package: "iproute2 / iproute（取决于发行版）"},
	{Name: "nft", Purpose: "隔离空间断线保护", Package: "nftables"},
	{Name: "nsenter", Purpose: "进入独立网络空间", Package: "util-linux"},
	{Name: "setpriv", Purpose: "最小权限进程切换", Package: "util-linux"},
	{Name: "systemctl", Purpose: "计划中的 systemd 安装管理", Package: "systemd"},
}

type source struct {
	platform, arch string
	uid            int
	lstat          func(string) (fs.FileInfo, error)
	read           func(string, int) ([]byte, error)
	lookPath       func(string) (string, error)
	service        func(context.Context, string) ([]byte, error)
	version        func(string) string
	process        func(int, []string) (string, string)
	sockets        func(context.Context, int, string) (string, []Listener)
	children       func(context.Context, int, []string) (string, []Process)
}

// Inspect performs no writes and makes no network requests. Missing evidence is
// not proof that a nonstandard/container installation does not exist.
func Inspect(ctx context.Context) Report {
	return inspect(ctx, source{platform: runtime.GOOS, arch: runtime.GOARCH, uid: os.Geteuid(), lstat: os.Lstat, read: readSmall, lookPath: exec.LookPath, service: queryService, version: binaryVersion, process: processIdentity, sockets: processSockets, children: processChildren})
}
func fileState(path, kind string, src source) string {
	info, err := src.lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "absent"
	}
	if err != nil {
		return "unreadable"
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	switch kind {
	case "directory":
		if info.IsDir() {
			return "present"
		}
	case "device":
		if info.Mode()&os.ModeCharDevice != 0 {
			return "present"
		}
	default:
		if info.Mode().IsRegular() {
			return "present"
		}
	}
	return "unexpected_type"
}
func inspect(ctx context.Context, src source) Report {
	r := Report{CheckedAt: time.Now().UTC(), Platform: src.platform + "/" + src.arch, Supported: src.platform == "linux", ReadOnly: true, Privileged: src.uid == 0,
		Distribution: "unknown", DistributionVersion: "unknown", TUN: "unknown", Systemd: "unknown", Dependencies: []Dependency{}, Components: []Component{}, Warnings: []string{}}
	if !r.Supported {
		r.Warnings = append(r.Warnings, "主机安装检测仅支持 Linux；当前结果不是 VPS 清单。")
		return r
	}
	if raw, err := src.read("/etc/os-release", 16384); err == nil {
		r.Distribution, r.DistributionVersion = parseOSRelease(raw)
	}
	r.TUN = fileState("/dev/net/tun", "device", src)
	r.Systemd = fileState("/run/systemd/system", "directory", src)
	for _, d := range dependencies {
		d.State = "available"
		if path, err := src.lookPath(d.Name); err != nil || !filepath.IsAbs(path) {
			d.State = "not_found"
		}
		r.Dependencies = append(r.Dependencies, d)
	}
	for _, spec := range specifications {
		c := Component{ID: spec.id, Name: spec.name, State: "not_detected", Version: "unknown", VersionState: "unknown", Services: []Service{}, Evidence: []Evidence{}}
		uncertain := false
		var binaries []string
		versions := map[string]bool{}
		for _, item := range spec.files {
			item.State = fileState(item.Path, item.Kind, src)
			if item.Kind == "binary" || item.Kind == "embedded_core" {
				binaries = append(binaries, item.Path)
				item.Version = "unknown"
				if item.State == "present" && src.version != nil && ctx.Err() == nil {
					item.Version = src.version(item.Path)
					if item.Version != "unknown" {
						item.VersionSource = "go_build_info"
						if item.Kind == "binary" {
							versions[item.Version] = true
						}
					}
				}
			}
			c.Evidence = append(c.Evidence, item)
			if item.State == "present" {
				c.State = "detected"
			}
			if item.State == "unreadable" || item.State == "symlink" || item.State == "unexpected_type" {
				uncertain = true
			}
		}
		for _, unit := range spec.units {
			service := Service{Unit: unit, State: "unavailable", Active: "unknown", ProcessState: "unknown", ListenerState: "unknown", Listeners: []Listener{}, ChildrenState: "unknown", ChildProcesses: []Process{}}
			if r.Systemd == "present" && ctx.Err() == nil {
				if raw, err := src.service(ctx, unit); err == nil {
					service = parseService(unit, raw)
				}
			}
			if service.State == "loaded" || service.State == "masked" {
				c.State = "detected"
			}
			if service.State == "unavailable" || service.State == "error" || service.State == "bad-setting" {
				uncertain = true
			}
			if service.MainPID > 0 && service.State == "loaded" && ctx.Err() == nil && src.process != nil {
				service.ProcessState, service.Executable = src.process(service.MainPID, binaries)
			} else if service.State == "not-found" || (service.State == "loaded" && service.Active == "inactive" && service.MainPID == 0) {
				service.ProcessState = "not_running"
			}
			if service.ProcessState == "matched" && ctx.Err() == nil {
				if src.sockets != nil {
					service.ListenerState, service.Listeners = src.sockets(ctx, service.MainPID, service.Executable)
				}
				if src.children != nil && ctx.Err() == nil {
					service.ChildrenState, service.ChildProcesses = src.children(ctx, service.MainPID, binaries)
				}
			}
			c.Services = append(c.Services, service)
		}
		if c.State == "not_detected" && uncertain {
			c.State = "unknown"
		}
		if len(versions) > 1 {
			c.VersionState = "conflicting"
		}
		if len(versions) == 1 {
			c.VersionState = "reported"
			for version := range versions {
				c.Version = version
			}
		}
		r.Components = append(r.Components, c)
	}
	r.Warnings = append(r.Warnings,
		"仅检查固定常见路径和 systemd 服务；自定义路径、容器、模板单元及未授权目录可能未覆盖。未检测到不等于未安装。",
		"存在配置/数据库或服务不代表已配置可用入口；本阶段不读取配置内容，不识别客户端凭据，不确认节点复用。",
		"版本仅从固定路径的 Go 构建元数据读取（程序自报，非签名认证）；未携带发布版本或存在版本冲突时保持未知。不执行核心/面板程序。",
		"未读取证书私钥、面板数据库、进程参数或环境变量；不会安装软件、重启服务或改写已有配置。",
		"监听归属只核对已匹配固定文件身份的 systemd MainPID 和最多16个直接子进程；关联其网络空间TCP监听inode与FD，并前后检查进程/网络空间。快照不证明公网可达、独占归属或可复用。")
	if ctx.Err() != nil {
		r.Warnings = append(r.Warnings, "检测达到时间上限或被取消；未完成项目保持未知。")
	}
	return r
}
func parseOSRelease(raw []byte) (string, string) {
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || (key != "ID" && key != "VERSION_ID") {
			continue
		}
		value = strings.Trim(value, "\"'")
		if safeLabel(value) {
			values[key] = value
		}
	}
	id, version := values["ID"], values["VERSION_ID"]
	if id == "" {
		id = "unknown"
	}
	if version == "" {
		version = "unknown"
	}
	return id, version
}
func safeLabel(value string) bool {
	if len(value) == 0 || len(value) > 80 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func parseService(unit string, raw []byte) Service {
	r := Service{Unit: unit, State: "unavailable", Active: "unknown", ProcessState: "unknown", ListenerState: "unknown", Listeners: []Listener{}, ChildrenState: "unknown", ChildProcesses: []Process{}}
	if len(raw) > 16384 {
		return r
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if _, exists := values[k]; exists {
			return r
		}
		values[k] = v
	}
	if values["Id"] != unit {
		return r
	}
	switch values["LoadState"] {
	case "loaded", "masked", "not-found", "error", "bad-setting":
		r.State = values["LoadState"]
	}
	switch values["ActiveState"] {
	case "active", "inactive", "failed", "activating", "deactivating", "reloading", "maintenance":
		r.Active = values["ActiveState"]
	}
	if pid, err := strconv.Atoi(values["MainPID"]); err == nil && pid > 0 && pid < 1<<30 {
		r.MainPID = pid
	}
	return r
}
