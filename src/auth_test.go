package main

import (
	"testing"
	"time"
)

func newTestAuth() *Auth {
	return &Auth{
		password: "secret",
		sessions: map[string]time.Time{},
		fails:    map[string]*loginFails{},
	}
}

// 连续失败达到阈值后，该 IP 应被冷却挡下。
func TestLoginThrottleBlocksAfterMaxFails(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.7"

	for i := 0; i < loginMaxFails; i++ {
		if a.blocked(ip) {
			t.Fatalf("第 %d 次失败前不该被封", i)
		}
		a.recordFail(ip)
	}
	if !a.blocked(ip) {
		t.Fatalf("连续 %d 次失败后应进入冷却", loginMaxFails)
	}
}

// 登录成功要清零，之前的失败不该累积到下一轮。
func TestLoginThrottleClearOnSuccess(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.8"

	for i := 0; i < loginMaxFails-1; i++ {
		a.recordFail(ip)
	}
	a.clearFails(ip)
	if a.blocked(ip) {
		t.Fatal("清零后不该被封")
	}
	// 清零后再错一次也不该立刻触发冷却
	a.recordFail(ip)
	if a.blocked(ip) {
		t.Fatal("清零后单次失败不该被封")
	}
}

// 不同 IP 的失败互不牵连。
func TestLoginThrottleIsolatesIPs(t *testing.T) {
	a := newTestAuth()
	for i := 0; i < loginMaxFails; i++ {
		a.recordFail("198.51.100.1")
	}
	if a.blocked("198.51.100.2") {
		t.Fatal("一个 IP 被封不该波及另一个 IP")
	}
}

// 冷却到期后应自动解封。
func TestLoginThrottleUnblocksAfterExpiry(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.9"
	for i := 0; i < loginMaxFails; i++ {
		a.recordFail(ip)
	}
	if !a.blocked(ip) {
		t.Fatal("应先进入冷却")
	}
	// 手动把封禁时间拨到过去，模拟冷却结束
	a.mu.Lock()
	a.fails[ip].blocked = time.Now().Add(-time.Second)
	a.mu.Unlock()
	if a.blocked(ip) {
		t.Fatal("冷却到期后应解封")
	}
}
