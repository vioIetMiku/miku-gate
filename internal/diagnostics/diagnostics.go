package diagnostics

import (
	"os"
	"os/exec"
	"runtime"
)

type Check struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type Report struct {
	Platform          string  `json:"platform"`
	ExecutorAvailable bool    `json:"executor_available"`
	Checks            []Check `json:"checks"`
}

func Inspect() Report {
	r := Report{Platform: runtime.GOOS + "/" + runtime.GOARCH, Checks: []Check{}}
	if runtime.GOOS != "linux" {
		r.Checks = append(r.Checks, Check{"platform", "Linux 网络环境", "unavailable", "当前可运行控制台与配置管理；真实 VPN 隧道只支持 Linux。"})
	} else {
		r.Checks = append(r.Checks, Check{"platform", "Linux 网络环境", "ok", "已检测到 Linux；本检查不修改网络。"})
		status, detail := "ok", "TUN 设备存在"
		if _, err := os.Stat("/dev/net/tun"); err != nil {
			status = "unavailable"
			detail = "未找到 /dev/net/tun，请确认宿主提供 TUN。"
		}
		r.Checks = append(r.Checks, Check{"tun", "TUN 设备", status, detail})
		for _, tool := range []string{"openvpn", "ip", "nft"} {
			status, detail = "ok", "命令可找到（尚未验证权限）"
			if _, err := exec.LookPath(tool); err != nil {
				status = "unavailable"
				detail = "未安装或不在 PATH 中"
			}
			r.Checks = append(r.Checks, Check{tool, tool, status, detail})
		}
	}
	r.Checks = append(r.Checks, Check{"executor", "特权网络执行器", "unavailable", "当前开发版本尚未接入。创建出口只保存配置，不监听代理、不建立隧道，不声称已提供断线保护。"})
	return r
}
