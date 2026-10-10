package httpapi

import (
	"context"
	"net/http"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/hostinventory"
)

func (a *Server) hostInventory(w http.ResponseWriter, r *http.Request, _ domain.Session) {
	a.mu.Lock()
	cached := a.hostReport
	a.mu.Unlock()
	if cached != nil && time.Since(cached.CheckedAt) < 15*time.Second {
		write(w, 200, cached)
		return
	}
	select {
	case a.hostSlot <- struct{}{}:
		defer func() { <-a.hostSlot }()
	default:
		fail(w, 429, "inventory_busy", "主机只读检测正在进行，请稍后再试")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	report := hostinventory.Inspect(ctx)
	a.mu.Lock()
	a.hostReport = &report
	a.mu.Unlock()
	write(w, 200, report)
}
