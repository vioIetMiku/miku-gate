package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeXrayConfigPorts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := `{
	  "inbounds": [
	    {"port": 15331, "protocol": "vless"},
	    {"port": 8080, "protocol": "trojan"},
	    {"port": "1000-2000", "protocol": "dokodemo-door"},
	    {"protocol": "no-port"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	used := map[int]bool{}
	mergeXrayConfigPorts(path, used)
	if !used[15331] || !used[8080] {
		t.Fatalf("数字端口应被并入: %v", used)
	}
	if used[1000] || used[2000] {
		t.Errorf("端口区间字符串不该被解析成端口: %v", used)
	}
	if len(used) != 2 {
		t.Errorf("只应有 2 个端口, got %v", used)
	}
}

func TestMergeXrayConfigPortsMissingFileIsSilent(t *testing.T) {
	used := map[int]bool{}
	mergeXrayConfigPorts("/nonexistent/definitely/not/here.json", used)
	if len(used) != 0 {
		t.Errorf("缺文件应静默, got %v", used)
	}
}

func TestMergeXrayConfigPortsBadJSONIsSilent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json at all"), 0600); err != nil {
		t.Fatal(err)
	}
	used := map[int]bool{}
	mergeXrayConfigPorts(path, used)
	if len(used) != 0 {
		t.Errorf("坏 JSON 应静默, got %v", used)
	}
}
