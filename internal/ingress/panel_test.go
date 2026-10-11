package ingress

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPersistentPanelTemplatePreservesClientsAndNonSelectedConfig(t *testing.T) {
	source, _ := FindSource("x-ui")
	rendered := []byte(xrayFixture)
	report, err := Inspect(source, rendered)
	if err != nil {
		t.Fatal(err)
	}
	template := []byte(`{"log":{"loglevel":"warning"},"inbounds":[],"outbounds":[{"tag":"direct","protocol":"freedom"}],"routing":{"rules":[{"type":"field","domain":["other.example"],"outboundTag":"direct"}]},"dns":{"servers":["1.1.1.1"]}}`)
	candidate, plan, err := PreparePanelTemplate(rendered, template, digest(rendered), digest(template), report.Inbounds[0].ID, Proxy{20000, "fixture", "fixture-secret"})
	if err != nil || plan.Applied || !plan.UDPBlocked {
		t.Fatal(plan, err)
	}
	before, _ := parseJSON(template)
	after, _ := parseJSON(candidate)
	for _, key := range []string{"inbounds", "dns", "log"} {
		if !reflect.DeepEqual(before[key], after[key]) {
			t.Fatal("modified", key)
		}
	}
	if len(array(object(after["routing"])["rules"])) != 3 || len(array(after["outbounds"])) != 3 {
		t.Fatal("persistent template route missing")
	}
	safe, _ := json.Marshal(plan)
	if strings.Contains(string(safe), "fixture-secret") {
		t.Fatal("plan leaked credential")
	}
	if _, _, err = PreparePanelTemplate(rendered, template, digest(rendered), strings.Repeat("0", 64), report.Inbounds[0].ID, Proxy{20000, "u", "p"}); err != ErrChanged {
		t.Fatal("template drift accepted")
	}
	if _, _, err = PreparePanelTemplate(rendered, candidate, digest(rendered), digest(candidate), report.Inbounds[0].ID, Proxy{20000, "u", "p"}); err == nil {
		t.Fatal("duplicate persistent reconciliation accepted")
	}
}
