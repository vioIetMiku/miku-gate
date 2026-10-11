package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mikugate/internal/ingress"
	"mikugate/internal/store"
	"mikugate/internal/wirejson"
	"path/filepath"
)

func runPanelPlan(mode string, input io.Reader, output io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(input, (3<<20)+1))
	if err != nil || len(raw) > 3<<20 {
		return ingress.ErrConfig
	}
	defer clear(raw)
	if mode == "panel-inspect" {
		source, _ := ingress.FindSource("x-ui")
		report, err := ingress.Inspect(source, raw)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(report)
	}
	var in struct {
		Data             string          `json:"data"`
		Instance         string          `json:"instance"`
		ExitID           string          `json:"exit_id"`
		Rendered         json.RawMessage `json:"rendered"`
		Template         json.RawMessage `json:"template"`
		ExpectedRendered string          `json:"expected_rendered"`
		ExpectedTemplate string          `json:"expected_template"`
		Inbound          string          `json:"inbound"`
		Proxy            ingress.Proxy   `json:"proxy"`
	}
	if wirejson.Decode(bytes.TrimSpace(raw), &in, 3<<20) != nil {
		return errors.New("面板计划需严格私有stdin JSON，不在参数/日志传认证材料")
	}
	if in.Data != "/var/lib/miku-gate/data" || in.Instance == "" || in.ExitID == "" {
		return errors.New("面板复用仅绑定已登记Miku实例/逻辑出口")
	}
	actual, err := store.InspectInstance(filepath.Join(in.Data, "miku-gate.db"))
	if err != nil || actual != in.Instance {
		return errors.New("Miku实例不匹配")
	}
	if err = store.VerifyExitProxy(filepath.Join(in.Data, "miku-gate.db"), in.ExitID, in.Proxy.Username, in.Proxy.Password, in.Proxy.Port); err != nil {
		return err
	}
	candidate, plan, err := ingress.PreparePanelTemplate(in.Rendered, in.Template, in.ExpectedRendered, in.ExpectedTemplate, in.Inbound, in.Proxy)
	if err != nil {
		return err
	}
	// Secret-bearing candidate ONLY to explicit local private pipe coordinator.
	return json.NewEncoder(output).Encode(struct {
		Template json.RawMessage `json:"template"`
		Plan     ingress.Plan    `json:"plan"`
	}{candidate, plan})
}
