package main

import (
	"sort"
	"time"
)

const (
	reconnectNoticeWindow    = 10 * time.Minute
	reconnectNoticeThreshold = 3
	maxReconnectHistory      = 128
	reconnectNoticeMessage   = "当前 VPS 与该地区的连接近期可能存在波动。为获得更平稳的使用体验，建议尝试其他地区。"
)

// Count outage-triggered reconnects, not candidate attempts, manual swaps, or
// service restarts. History belongs to the logical exit across node changes.
type StabilityNotice struct {
	Reconnects    int       `json:"reconnects"`
	WindowSeconds int       `json:"window_seconds"`
	Until         time.Time `json:"until"`
	Message       string    `json:"message"`
}

func recentReconnects(events []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-reconnectNoticeWindow)
	recent := make([]time.Time, 0, len(events))
	for _, event := range events {
		if event.After(cutoff) && !event.After(now) {
			recent = append(recent, event)
		}
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].Before(recent[j]) })
	if len(recent) > maxReconnectHistory {
		recent = recent[len(recent)-maxReconnectHistory:]
	}
	return recent
}

func (t *Tunnel) recordAutoReconnect(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.autoReconnects = recentReconnects(append(t.autoReconnects, now), now)
}

func (t *Tunnel) restoreReconnectHistory(events []time.Time, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.autoReconnects = recentReconnects(events, now)
}

func (t *Tunnel) reconnectHistory(now time.Time) []time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.autoReconnects = recentReconnects(t.autoReconnects, now)
	return append([]time.Time(nil), t.autoReconnects...)
}

func (t *Tunnel) stabilityNotice(now time.Time) *StabilityNotice {
	recent := t.reconnectHistory(now)
	if len(recent) <= reconnectNoticeThreshold {
		return nil
	}
	// The notice expires when fewer than four events remain in the window.
	return &StabilityNotice{Reconnects: len(recent), WindowSeconds: int(reconnectNoticeWindow.Seconds()), Until: recent[len(recent)-reconnectNoticeThreshold-1].Add(reconnectNoticeWindow), Message: reconnectNoticeMessage}
}
