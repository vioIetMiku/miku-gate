package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func awaitQuickCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("continuous race did not make progress")
		}
		time.Sleep(time.Millisecond)
	}
}

func workingQuickEntry(t *Tunnel) error { t.EntryReady = true; return nil }
func stopQuickFixture(t *Tunnel)        { t.Status = "stopped" }

func TestDev39RaceContinuesBeyondInitialWindowAndReusesSlots(t *testing.T) {
	useSettings(t)
	for _, limit := range []int{0, 11, 14} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			m := NewManager(limit, t.TempDir())
			// Fail beyond the larger dev40 initial windows. Cancellation may
			// prevent an already reserved loser from reaching connect at all.
			thresholds := map[string]int{"JP": 18, "KR": 12, "US": 12}
			for r, region := range quickRegions {
				for i := 1; i <= 24; i++ {
					m.nodes = append(m.nodes, Node{HostName: fmt.Sprintf("%s-%d", region.Code, i), CountryCode: region.Code, IP: fmt.Sprintf("198.51.%d.%d", 100+r, i)})
				}
			}
			job := quickFixtureJob(m)
			targets := m.quickTargets(job, 77)
			limits := m.quickRaceLimits(targets)
			var mu sync.Mutex
			attempted, active, peak := map[string]int{}, map[string]int{}, map[string]int{}
			seen := map[string]bool{}
			cleaned := m.runQuickRace(job, targets, 77, quickRaceOps{
				retryDelay: time.Hour,
				connect: func(tunnel *Tunnel) error {
					code := tunnel.Node.CountryCode
					mu.Lock()
					if seen[tunnel.Node.HostName] {
						t.Errorf("retried a failed endpoint before exhausting the pool: %s", tunnel.Node.HostName)
					}
					seen[tunnel.Node.HostName] = true
					attempted[code]++
					active[code]++
					if active[code] > peak[code] {
						peak[code] = active[code]
					}
					mu.Unlock()
					defer func() { mu.Lock(); active[code]--; mu.Unlock() }()
					n, _ := strconv.Atoi(strings.Split(tunnel.Node.HostName, "-")[1])
					if n <= thresholds[code] {
						return errors.New("fixture unreachable")
					}
					tunnel.ExitIP = tunnel.Node.IP
					return nil
				},
				entry: workingQuickEntry, stop: stopQuickFixture,
			})
			awaitRaceValue(t, cleaned)
			counts := map[string]int{}
			for _, tunnel := range m.visibleTunnels() {
				counts[tunnel.preferredRegion()]++
			}
			mu.Lock()
			defer mu.Unlock()
			for _, region := range quickRegions {
				if counts[region.Code] != region.Count || attempted[region.Code] <= region.Count || peak[region.Code] > limits[region.Code] {
					t.Fatal("quota, rolling attempts or worker ceiling violated", limit, counts, attempted, peak, limits)
				}
			}
			if len(m.allTunnels()) != 11 {
				t.Fatal("failed candidates retained slots", len(m.allTunnels()))
			}
		})
	}
}

func TestDev39TimeoutReplacesCandidateInsteadOfEndingJob(t *testing.T) {
	useSettings(t)
	m := NewManager(1, t.TempDir())
	for i := 1; i <= 4; i++ {
		m.nodes = append(m.nodes, Node{HostName: fmt.Sprint(i), CountryCode: "JP", IP: fmt.Sprintf("198.51.100.%d", i)})
	}
	var mu sync.Mutex
	tried := 0
	start := time.Now()
	cleaned := m.runQuickRace(quickFixtureJob(m), []quickTarget{{"JP", 4}}, 77, quickRaceOps{
		timeout: 20 * time.Millisecond, retryDelay: time.Hour,
		connect: func(tunnel *Tunnel) error {
			mu.Lock()
			tried++
			mu.Unlock()
			if tunnel.Node.HostName != "4" {
				<-tunnel.connectionContext().Done()
				return tunnel.connectionContext().Err()
			}
			tunnel.ExitIP = tunnel.Node.IP
			return nil
		}, entry: workingQuickEntry, stop: stopQuickFixture,
	})
	awaitRaceValue(t, cleaned)
	mu.Lock()
	defer mu.Unlock()
	if tried != 4 || len(m.visibleTunnels()) != 1 || time.Since(start) < 50*time.Millisecond {
		t.Fatal("per-candidate deadline ended the race", tried, m.visibleTunnels())
	}
}

func TestDev39EmptyCountriesWaitForRefreshWhileCompletedCountryStops(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	for i := 1; i <= 6; i++ {
		m.nodes = append(m.nodes, Node{HostName: fmt.Sprintf("US-%d", i), CountryCode: "US", IP: fmt.Sprintf("198.51.102.%d", i)})
	}
	job := quickFixtureJob(m)
	targets := m.quickTargets(job, 77)
	if len(targets) != 11 {
		t.Fatal("empty countries lost their targets")
	}
	refreshStarted, releaseRefresh := make(chan struct{}, 1), make(chan struct{})
	var mu sync.Mutex
	started := map[string]int{}
	returned := make(chan (<-chan struct{}), 1)
	go func() {
		returned <- m.runQuickRace(job, targets, 77, quickRaceOps{
			retryDelay: 5 * time.Millisecond,
			connect: func(tunnel *Tunnel) error {
				mu.Lock()
				started[tunnel.Node.CountryCode]++
				mu.Unlock()
				tunnel.ExitIP = tunnel.Node.IP
				return nil
			},
			entry: workingQuickEntry, stop: stopQuickFixture,
			refresh: func(ctx context.Context) error {
				refreshStarted <- struct{}{}
				select {
				case <-releaseRefresh:
				case <-ctx.Done():
					return ctx.Err()
				}
				m.mu.Lock()
				for r, region := range quickRegions[:2] {
					for i := 1; i <= region.Count; i++ {
						m.nodes = append(m.nodes, Node{HostName: fmt.Sprintf("%s-%d", region.Code, i), CountryCode: region.Code, IP: fmt.Sprintf("198.51.%d.%d", 100+r, i)})
					}
				}
				m.mu.Unlock()
				return nil
			},
		})
	}()
	awaitRaceValue(t, refreshStarted)
	awaitQuickCondition(t, func() bool {
		v := job.View()
		return v.Steps[12].Status == "ok" && v.Steps[13].Status == "ok" && v.Steps[14].Status == "ok"
	})
	for _, step := range job.View().Steps[4:12] {
		if step.Status != "running" {
			t.Fatal("empty country ended instead of waiting", step)
		}
	}
	select {
	case <-returned:
		t.Fatal("one completed country ended the job")
	default:
	}
	close(releaseRefresh)
	cleaned := awaitRaceValue(t, returned)
	awaitRaceValue(t, cleaned)
	if len(m.visibleTunnels()) != 11 {
		t.Fatal("refreshed countries were not filled")
	}
	mu.Lock()
	defer mu.Unlock()
	if started["US"] > 6 || started["JP"] != 5 || started["KR"] != 3 {
		t.Fatal("completed country restarted or quotas overflowed", started)
	}
}

func TestDev39RefreshFailureStillRetriesCachedCandidates(t *testing.T) {
	useSettings(t)
	m := NewManager(1, t.TempDir())
	m.nodes = []Node{{HostName: "recovered", CountryCode: "JP", IP: "198.51.100.1"}}
	attempts, refreshes := 0, 0
	cleaned := m.runQuickRace(quickFixtureJob(m), []quickTarget{{"JP", 4}}, 77, quickRaceOps{
		retryDelay: time.Millisecond,
		connect: func(tunnel *Tunnel) error {
			attempts++
			if attempts == 1 {
				return errors.New("temporarily full")
			}
			tunnel.ExitIP = tunnel.Node.IP
			return nil
		},
		entry: workingQuickEntry, stop: stopQuickFixture,
		refresh: func(context.Context) error { refreshes++; return errors.New("source unavailable") },
	})
	awaitRaceValue(t, cleaned)
	if attempts != 2 || refreshes != 1 || len(m.visibleTunnels()) != 1 {
		t.Fatal("cached recovery was abandoned", attempts, refreshes)
	}
}

func TestDev39StopCancelsWaitingRaceAndPreservesWinners(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	for i := 1; i <= 6; i++ {
		m.nodes = append(m.nodes, Node{HostName: fmt.Sprint(i), CountryCode: "US", IP: fmt.Sprintf("198.51.102.%d", i)})
	}
	job := quickFixtureJob(m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.quickJob, m.quickCancel = job, cancel
	refreshed := make(chan struct{}, 1)
	returned := make(chan (<-chan struct{}), 1)
	go func() {
		returned <- m.runQuickRace(job, m.quickTargets(job, 77), 77, quickRaceOps{
			ctx: ctx, retryDelay: time.Millisecond,
			connect: func(tunnel *Tunnel) error { tunnel.ExitIP = tunnel.Node.IP; return nil }, entry: workingQuickEntry, stop: stopQuickFixture,
			refresh: func(ctx context.Context) error { refreshed <- struct{}{}; <-ctx.Done(); return ctx.Err() },
		})
	}()
	awaitRaceValue(t, refreshed)
	awaitQuickCondition(t, func() bool { return job.View().Steps[14].Status == "ok" })
	for _, tc := range []struct {
		method, id string
		status     int
	}{{"GET", job.ID(), 405}, {"POST", "other-task", 409}, {"POST", job.ID(), 202}} {
		w := httptest.NewRecorder()
		apiQuickSetupStop(m).ServeHTTP(w, httptest.NewRequest(tc.method, "/api/quick-setup/stop?id="+tc.id, nil))
		if w.Code != tc.status {
			t.Fatal("incorrect stop response", tc, w.Code)
		}
	}
	cleaned := awaitRaceValue(t, returned)
	awaitRaceValue(t, cleaned)
	if len(m.allTunnels()) != 3 {
		t.Fatal("stop removed winners or leaked candidates", len(m.allTunnels()))
	}
	for _, tunnel := range m.visibleTunnels() {
		if tunnel.preferredRegion() != "US" || tunnel.connectionContext().Err() != nil {
			t.Fatal("winner inherited cancellation")
		}
	}
	for _, step := range job.View().Steps[4:12] {
		if step.Status != "failed" {
			t.Fatal("canceled targets still running", step)
		}
	}
}

func TestDev39ReplacementWaitsForCleanupBeforeReusingSlot(t *testing.T) {
	useSettings(t)
	m := NewManager(1, t.TempDir())
	m.nodes = []Node{{HostName: "bad", CountryCode: "JP"}, {HostName: "good", CountryCode: "JP", IP: "198.51.100.2"}}
	cleaning, release := make(chan struct{}), make(chan struct{})
	started := make(chan string, 2)
	returned := make(chan (<-chan struct{}), 1)
	go func() {
		returned <- m.runQuickRace(quickFixtureJob(m), []quickTarget{{"JP", 4}}, 77, quickRaceOps{
			retryDelay: time.Hour,
			connect: func(tunnel *Tunnel) error {
				started <- tunnel.Node.HostName
				if tunnel.Node.HostName == "bad" {
					return errors.New("unreachable")
				}
				tunnel.ExitIP = tunnel.Node.IP
				return nil
			},
			entry: workingQuickEntry,
			stop:  func(tunnel *Tunnel) { close(cleaning); <-release; tunnel.Status = "stopped" },
		})
	}()
	if awaitRaceValue(t, started) != "bad" {
		t.Fatal("unexpected initial worker")
	}
	awaitRaceValue(t, cleaning)
	select {
	case <-started:
		t.Fatal("slot reused before cleanup")
	default:
	}
	close(release)
	if awaitRaceValue(t, started) != "good" {
		t.Fatal("next candidate was abandoned")
	}
	cleaned := awaitRaceValue(t, returned)
	awaitRaceValue(t, cleaned)
	if len(m.visibleTunnels()) != 1 {
		t.Fatal("replacement did not survive cleanup")
	}
}

func TestDev39SourceRequestObservesCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)
	go func() { _, err := fetchNodesFromContext(ctx, server.URL, "", time.Minute); returned <- err }()
	awaitRaceValue(t, started)
	cancel()
	if awaitRaceValue(t, returned) == nil {
		t.Fatal("canceled source request succeeded")
	}
}
