package ingress

import "encoding/json"

// PreparePanelTemplate modifies the panel's PERSISTENT Xray template, not its
// generated config. Selected inbound identity is taken from the exact rendered
// snapshot; clients/listeners/TLS settings and every unselected field remain.
func PreparePanelTemplate(rendered, template []byte, expectedRendered, expectedTemplate, inboundID string, proxy Proxy) ([]byte, Plan, error) {
	if digest(template) != expectedTemplate || !ValidSHA(expectedTemplate) {
		return nil, Plan{}, ErrChanged
	}
	source, _ := FindSource("x-ui")
	candidate, plan, err := Prepare(source, rendered, expectedRendered, inboundID, proxy)
	if err != nil {
		return nil, Plan{}, err
	}
	target, err := parseJSON(template)
	if err != nil {
		return nil, Plan{}, err
	}
	prepared, err := parseJSON(candidate)
	if err != nil {
		return nil, Plan{}, err
	}
	if array(target["outbounds"]) == nil {
		return nil, Plan{}, ErrConfig
	}
	for _, value := range array(target["outbounds"]) {
		if len(text(object(value)["tag"])) >= 10 && text(object(value)["tag"])[:10] == "miku-gate-" {
			return nil, Plan{}, ErrSelection
		}
	}
	routing := object(target["routing"])
	if routing == nil {
		if target["routing"] != nil {
			return nil, Plan{}, ErrConfig
		}
		routing = map[string]any{}
	}
	rules := array(routing["rules"])
	if value, exists := routing["rules"]; exists && (value == nil || rules == nil) {
		return nil, Plan{}, ErrConfig
	}
	additions := []any{}
	for _, value := range array(prepared["outbounds"]) {
		tag := text(object(value)["tag"])
		if tag == plan.OutboundTag || tag == plan.OutboundTag+"-udp-block" {
			additions = append(additions, value)
		}
	}
	if len(additions) != 2 {
		return nil, Plan{}, ErrConfig
	}
	generatedRules := array(object(prepared["routing"])["rules"])
	if len(generatedRules) < 2 {
		return nil, Plan{}, ErrConfig
	}
	routing["rules"] = append(generatedRules[:2:2], rules...)
	target["routing"] = routing
	target["outbounds"] = append(array(target["outbounds"]), additions...)
	raw, err := json.MarshalIndent(target, "", "  ")
	if err != nil || len(raw) > MaxConfig {
		return nil, Plan{}, ErrConfig
	}
	raw = append(raw, '\n')
	plan.CandidateSHA256 = digest(raw)
	plan.Warnings = append(plan.Warnings, "候选是面板持久Xray模板，须通过面板API保存并重新读取生成配置验证；不是直接覆盖bin/config.json。")
	return raw, plan, nil
}
