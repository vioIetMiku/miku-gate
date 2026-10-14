package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

const gateSnapshotIndex = "https://raw.githubusercontent.com/GeorgeXie2333/vpngate-list-mirror/main/latest.json"

func fetchGateSnapshot(ctx context.Context) ([]Node, error) {
	raw, err := publicJSON(ctx, gateSnapshotIndex, 64<<10)
	if err != nil {
		return nil, err
	}
	var index struct {
		Version int                       `json:"schema_version"`
		Commit  string                    `json:"data_commit"`
		Fetched time.Time                 `json:"fetched_at"`
		Files   map[string]PoolDescriptor `json:"files"`
	}
	if json.Unmarshal(raw, &index) != nil || index.Version != 1 || index.Fetched.IsZero() || time.Since(index.Fetched) > 24*time.Hour || index.Fetched.After(time.Now().Add(5*time.Minute)) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(index.Commit) {
		return nil, fmt.Errorf("快照索引无效或过期")
	}
	file, ok := index.Files["data/vpngate.csv"]
	if !ok || file.Bytes < 1 || file.Bytes > 16<<20 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(file.SHA256) {
		return nil, fmt.Errorf("快照文件描述无效")
	}
	for _, host := range []string{"https://cdn.jsdelivr.net/gh/GeorgeXie2333/vpngate-list-mirror@", "https://fastly.jsdelivr.net/gh/GeorgeXie2333/vpngate-list-mirror@", "https://gcore.jsdelivr.net/gh/GeorgeXie2333/vpngate-list-mirror@", "https://raw.githubusercontent.com/GeorgeXie2333/vpngate-list-mirror/"} {
		raw, err = publicJSON(ctx, host+index.Commit+"/data/vpngate.csv", file.Bytes)
		if err != nil || len(raw) != file.Bytes {
			continue
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			continue
		}
		nodes, err := parseNodeCSV(string(raw))
		if err != nil {
			continue
		}
		for i := range nodes {
			nodes[i].Source = "开源定时快照"
			nodes[i].Priority = 2
		}
		return nodes, nil
	}
	return nil, fmt.Errorf("快照下载或SHA256校验失败")
}
