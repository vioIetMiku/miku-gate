package main

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type healthTask struct {
	Tunnel    *Tunnel
	Slot      int
	Node      Node
	ExitIP    string
	Namespace string
	ReadyAt   time.Time
}
type healthResult struct {
	Task             healthTask
	Healthy          bool
	MS               float64
	Target           string
	Geo              *IPInfo
	IdentityMismatch bool
}

func parseTunnelTiming(raw []byte) (string, float64, error) {
	const marker = "\nMIKU_CONNECT:"
	text := string(raw)
	at := strings.LastIndex(text, marker)
	if at < 0 {
		return "", 0, fmt.Errorf("隧道检测缺少时间信息")
	}
	parts := strings.Split(strings.TrimSpace(text[at+len(marker):]), ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("隧道检测时间格式无效")
	}
	connect, e1 := strconv.ParseFloat(parts[0], 64)
	dns, e2 := strconv.ParseFloat(parts[1], 64)
	if e1 != nil || e2 != nil || connect < dns || dns < 0 || math.IsNaN(connect) || math.IsInf(connect, 0) || math.IsNaN(dns) || math.IsInf(dns, 0) {
		return "", 0, fmt.Errorf("隧道检测时间无效")
	}
	return strings.TrimSpace(text[:at]), (connect - dns) * 1000, nil
}

func fetchTunnelMeasurement(namespace, address string) (string, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := cmdOutput(exec.CommandContext(ctx, "ip", "netns", "exec", namespace, "curl",
		"-4", "--interface", "tun0", "--noproxy", "*", "--proto", "=https",
		"--fail", "--silent", "--show-error", "--connect-timeout", "2", "--max-time", "3", "--max-filesize", "16384",
		"--write-out", "\nMIKU_CONNECT:%{time_connect}:%{time_namelookup}\n", address))
	if err != nil {
		return "", 0, fmt.Errorf("隧道HTTPS检测暂不可用")
	}
	if len(out) > 17<<10 {
		return "", 0, fmt.Errorf("隧道检测响应过大")
	}
	return parseTunnelTiming(out)
}

func probeTunnelHealth(task healthTask) healthResult {
	return probeTunnelHealthWith(task, fetchTunnelMeasurement)
}

func probeTunnelHealthWith(task healthTask, fetch func(string, string) (string, float64, error)) healthResult {
	result := healthResult{Task: task}
	for _, address := range []string{"https://api.ipify.org", "https://ping0.cc/geo", "https://checkip.amazonaws.com"} {
		body, ms, err := fetch(task.Namespace, address)
		if err != nil {
			continue
		}
		got := ""
		if address == "https://ping0.cc/geo" {
			geo, e := parsePing0Geo(body)
			if e != nil {
				continue
			}
			got = geo.IP
			result.Geo = &geo
		} else {
			got, err = publicLookupIP(body)
			if err != nil {
				continue
			}
		}
		// A real response from the wrong exit is never masked by another site.
		if got != task.ExitIP {
			result.IdentityMismatch = true
			return result
		}
		if ms < 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
			continue
		}
		result.Healthy, result.MS, result.Target = true, ms, address
		return result
	}
	return result
}

func (m *Manager) healthSnapshot(t *Tunnel) (healthTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.tunnels[t.Slot] != t || t.Status != "up" || t.ExitIP == "" {
		return healthTask{}, false
	}
	return healthTask{Tunnel: t, Slot: t.Slot, Node: t.Node, ExitIP: t.ExitIP, Namespace: t.nsName(), ReadyAt: t.readyAt}, true
}

func (m *Manager) healthResultCurrent(result healthResult) bool {
	t := result.Task.Tunnel
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tunnels[result.Task.Slot] == t && t.Status == "up" && t.ExitIP == result.Task.ExitIP && latencyKey(t.Node) == latencyKey(result.Task.Node) && t.readyAt.Equal(result.Task.ReadyAt)
}

func (m *Manager) scheduleEntryRepair(t *Tunnel) {
	t.mu.Lock()
	if t.repairPending {
		t.mu.Unlock()
		return
	}
	t.repairPending = true
	t.mu.Unlock()
	go func() {
		defer func() { t.mu.Lock(); t.repairPending = false; t.mu.Unlock() }()
		if err := m.ensureTunnelEntry(t); err != nil {
			t.EntryError = err.Error()
		}
	}()
}
