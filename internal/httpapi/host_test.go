package httpapi

import (
	"context"
	"encoding/json"
	"mikugate/internal/domain"
	"mikugate/internal/hostinventory"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInventoryCacheAndBusyAreBounded(t *testing.T) {
	a := &Server{hostSlot: make(chan struct{}, 1)}
	a.hostSlot <- struct{}{}
	request := httptest.NewRequest("GET", "http://localhost/api/host/inventory", nil)
	w := httptest.NewRecorder()
	a.hostInventory(w, request, domain.Session{})
	if w.Code != 429 {
		t.Fatal("no in-flight limit")
	}
	cached := hostinventory.Report{CheckedAt: time.Now().UTC(), ReadOnly: true, Platform: "fixture"}
	a.hostReport = &cached
	w = httptest.NewRecorder()
	a.hostInventory(w, request, domain.Session{})
	if w.Code != 200 {
		t.Fatal("cached response blocked")
	}
	var got hostinventory.Report
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Platform != "fixture" {
		t.Fatal("cache not used")
	}
	a.hostReport = nil
	<-a.hostSlot
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = httptest.NewRecorder()
	a.hostInventory(w, request.WithContext(ctx), domain.Session{})
	if len(a.hostSlot) != 0 {
		t.Fatal("inspection slot leaked")
	}
}
