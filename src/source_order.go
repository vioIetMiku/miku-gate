package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

func orderedSourceURLs() []string {
	defaults := append([]string{gatePoolIndex, gateSnapshotIndex}, configuredSources()...)
	seen, out := map[string]bool{}, []string{}
	allowed := map[string]bool{}
	for _, source := range defaults {
		allowed[source] = true
	}
	for _, source := range append(getWebSettings().VPNGateSourceOrder, defaults...) {
		if allowed[source] && !seen[source] {
			seen[source] = true
			out = append(out, source)
		}
	}
	return out
}

func configuredSourceRank(source string) int {
	for i, value := range orderedSourceURLs() {
		if source == value {
			return (i + 1) * 10
		}
	}
	return 10000
}

func sourceCatalog() []SourceReport {
	known := map[string]SourceReport{}
	for _, report := range getSourceReports() {
		known[report.URL] = report
	}
	out := []SourceReport{}
	for i, source := range orderedSourceURLs() {
		value, found := known[source]
		if !found {
			value = SourceReport{URL: source, Name: "自定义CSV来源"}
		}
		if source == gatePoolIndex {
			value.Name = "开源滚动池（近期TCP检测）"
		}
		if source == gateSnapshotIndex {
			value.Name = "开源定时快照（校验提交与哈希）"
		}
		if source == vpngateAPI {
			value.Name = "官方API（兜底）"
		}
		value.Priority = i + 1
		out = append(out, value)
	}
	return out
}

// Source order also changes the current candidate pool immediately; there is
// no need to wait for another scheduled download for the preference to apply.
func (m *Manager) reorderNodes() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.nodes {
		source := m.nodes[i].PrioritySource
		if source == "" {
			source = m.nodes[i].Source
		}
		rank := configuredSourceRank(source)
		if m.nodes[i].Pool != nil && m.nodes[i].Priority%10 != 0 {
			rank++
		}
		m.nodes[i].Priority = rank
	}
	sort.SliceStable(m.nodes, func(i, j int) bool {
		if nodePriority(m.nodes[i]) != nodePriority(m.nodes[j]) {
			return nodePriority(m.nodes[i]) < nodePriority(m.nodes[j])
		}
		return m.nodes[i].SpeedMbps > m.nodes[j].SpeedMbps
	})
}

func apiSourceAdd(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "只接受POST"})
			return
		}
		var in struct {
			URL string `json:"url"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || !validSourceURL(strings.TrimSpace(in.URL)) {
			writeJSON(w, 400, map[string]string{"error": "请填写公网HTTP/HTTPS的VPN Gate CSV链接"})
			return
		}
		source := strings.TrimSpace(in.URL)
		key := ""
		if source == mirrorURL() {
			key = mirrorAccessKey()
		}
		nodes, err := fetchNodesFrom(source, key, 12*time.Second)
		if err != nil || len(nodes) == 0 {
			writeJSON(w, 400, map[string]string{"error": "链接无法读取有效OpenVPN节点，请检查来源后重试"})
			return
		}
		webSettingsMu.Lock()
		found := false
		for _, old := range webSettingsCur.VPNGateSources {
			if old == source {
				found = true
			}
		}
		if !found && source != vpngateAPI {
			webSettingsCur.VPNGateSources = append(webSettingsCur.VPNGateSources, source)
		}
		webSettingsMu.Unlock()
		if err := saveWebSettings(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "来源保存失败"})
			return
		}
		for i := range nodes {
			nodes[i].Priority = configuredSourceRank(source)
			nodes[i].Source = source
			nodes[i].PrioritySource = source
		}
		m.mu.Lock()
		m.nodes = mergeSourceNodes(m.nodes, nodes)
		m.fetched = time.Now()
		m.mu.Unlock()
		m.reorderNodes()
		sourceReports.Lock()
		for i := range sourceReports.Values {
			if sourceReports.Values[i].URL == source {
				sourceReports.Values = append(sourceReports.Values[:i], sourceReports.Values[i+1:]...)
				break
			}
		}
		sourceReports.Values = append(sourceReports.Values, SourceReport{URL: source, Count: len(nodes), Fetched: time.Now(), Name: "自定义CSV来源"})
		sourceReports.Unlock()
		writeJSON(w, 200, map[string]any{"count": len(nodes), "source_catalog": sourceCatalog(), "vpngate_sources": getWebSettings().VPNGateSources})
	}
}

func apiSourceOrder(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "只接受POST"})
			return
		}
		var in struct {
			Order []string `json:"order"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&in) != nil {
			writeJSON(w, 400, map[string]string{"error": "请求格式无效"})
			return
		}
		allowed := map[string]bool{}
		for _, source := range orderedSourceURLs() {
			allowed[source] = true
		}
		for _, source := range in.Order {
			if !allowed[source] {
				writeJSON(w, 400, map[string]string{"error": "请先添加此来源，再调整顺序"})
				return
			}
		}
		webSettingsMu.Lock()
		webSettingsCur.VPNGateSourceOrder = append([]string{}, in.Order...)
		webSettingsMu.Unlock()
		if err := saveWebSettings(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "无法保存来源顺序"})
			return
		}
		m.reorderNodes()
		writeJSON(w, 200, map[string]any{"source_catalog": sourceCatalog()})
	}
}
