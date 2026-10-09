package diagnostics

import "testing"

func TestReportCannotClaimExecutorReadiness(t *testing.T) {
	report := Inspect()
	if report.ExecutorAvailable {
		t.Fatal("unimplemented executor reported available")
	}
	if report.Platform == "" || len(report.Checks) < 2 {
		t.Fatal("missing diagnostic details")
	}
	found := false
	for _, check := range report.Checks {
		if check.ID == "executor" {
			found = true
			if check.Status != "unavailable" {
				t.Fatal("executor status is misleading")
			}
		}
	}
	if !found {
		t.Fatal("missing capability limitation")
	}
}
