package main

import (
	"encoding/json"
	"os"
)

// externalXrayConfigs 是其它工具装的系统级 Xray 配置，fanout 自己不写，
// 但要避开它们占用的入站端口，免得两边端口撞车、Xray 起不来。
//
// 目前覆盖 byJoey/xray-cf-lite：它把 Xray 装成系统服务，
// 配置固定落在 /usr/local/etc/xray/config.json。
var externalXrayConfigs = []string{
	"/usr/local/etc/xray/config.json",
}

// externalUsedPorts 读取外部 Xray 配置里 inbounds 的监听端口。
//
// 只读不写，任何一个文件不存在或解析失败都静默跳过，
// 保证 fanout 在没有这些工具的机器上行为完全不变。
func externalUsedPorts() map[int]bool {
	used := map[int]bool{}
	for _, path := range externalXrayConfigs {
		mergeXrayConfigPorts(path, used)
	}
	return used
}

func mergeXrayConfigPorts(path string, used map[int]bool) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cfg struct {
		Inbounds []struct {
			// port 在 Xray 配置里可能是数字，也可能是 "1000-2000" 这类端口段字符串。
			// 用 RawMessage 逐条宽松解析：数字就取，非数字就跳过，
			// 避免一个端口段字符串让整个数组解析失败。
			Port json.RawMessage `json:"port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return
	}
	for _, ib := range cfg.Inbounds {
		var p int
		if err := json.Unmarshal(ib.Port, &p); err == nil && p > 0 && p < 65536 {
			used[p] = true
		}
	}
}
