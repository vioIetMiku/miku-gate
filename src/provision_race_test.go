package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestProvisionRacePublishesFirstThreeAndClosesSurplus(t *testing.T) {
	for _, code := range []string{"JP", "KR", "DE"} {
		for _, available := range []int{3, 12} {
			t.Run(fmt.Sprintf("%s/%d", code, available), func(t *testing.T) {
				useSettings(t)
				m := NewManager(0, t.TempDir())
				release := map[string]chan struct{}{}
				for i := 1; i <= available; i++ {
					host := fmt.Sprintf("%s-%d", code, i)
					m.nodes = append(m.nodes, Node{HostName: host, CountryCode: code, IP: fmt.Sprintf("198.51.100.%d", i)})
					release[host] = make(chan struct{})
				}
				// An alias of the first endpoint must not get a second connection.
				m.nodes = append(m.nodes, Node{HostName: "alias", CountryCode: code, IP: m.nodes[0].IP})
				targets := []quickTarget{{code, 0}, {code, 1}, {code, 2}}
				job := m.jobs.New("ordinary create", []string{"first", "second", "third"})
				started := make(chan string, 32)
				stopped := make(chan string, 32)
				returned := make(chan (<-chan struct{}), 1)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() {
					returned <- m.runExitRace(job, targets, 0, quickRaceOps{
						ctx: ctx, retryDelay: time.Hour,
						connect: func(tunnel *Tunnel) error {
							started <- tunnel.Node.HostName
							select {
							case <-release[tunnel.Node.HostName]:
								tunnel.ExitIP = tunnel.Node.IP
								return nil
							case <-tunnel.connectionContext().Done():
								return tunnel.connectionContext().Err()
							}
						},
						entry: workingQuickEntry,
						stop:  func(tunnel *Tunnel) { stopped <- tunnel.Node.HostName; stopQuickFixture(tunnel) },
					})
				}()
				window := min(available, 9)
				for i := 0; i < window; i++ {
					awaitRaceValue(t, started)
				}
				if len(m.allTunnels()) != window || len(m.visibleTunnels()) != 0 {
					t.Fatal("wrong initial window or speculative card exposed")
				}
				var winners []string
				for i := 0; i < 3; i++ {
					host := fmt.Sprintf("%s-%d", code, window-i)
					winners = append(winners, host)
					close(release[host])
					step := i
					awaitQuickCondition(t, func() bool { return job.View().Steps[step].Status == "ok" })
					if got := len(m.visibleTunnels()); got != i+1 {
						t.Fatal("winner was not published immediately", got)
					}
				}
				awaitRaceValue(t, awaitRaceValue(t, returned))
				cancel()
				visible := m.visibleTunnels()
				if len(visible) != 3 || len(m.allTunnels()) != 3 || len(stopped) != window-3 || job.View().QuickResult != nil {
					t.Fatal("ordinary race kept extras or acquired preset results", len(visible), len(stopped))
				}
				for i, tunnel := range visible {
					if tunnel.Node.HostName != winners[i] || tunnel.connectionContext().Err() != nil {
						t.Fatal("winner replaced or canceled", tunnel.Node.HostName, winners)
					}
				}
				raw, err := os.ReadFile(statePath(m.workDir))
				var saved persistedState
				if err != nil || json.Unmarshal(raw, &saved) != nil || len(saved.Tunnels) != 3 {
					t.Fatal("candidate leaked into restart state", err)
				}
			})
		}
	}
}

func TestProvisionRaceReservesEachRequestedCountry(t *testing.T) {
	m := NewManager(0, t.TempDir())
	targets := []quickTarget{{"JP", 0}, {"JP", 1}, {"JP", 2}, {"KR", 3}, {"KR", 4}, {"KR", 5}}
	if limits := m.quickRaceLimits(targets); len(limits) != 2 || limits["JP"] != 9 || limits["KR"] != 9 {
		t.Fatal("ordinary request did not get nine candidates per country", limits)
	}
	m.maxSlots = 8
	if limits := m.quickRaceLimits(targets); limits["JP"] != 4 || limits["KR"] != 4 {
		t.Fatal("spare workers starved a requested country", limits)
	}
}

func TestProvisionStopCancelsOnlyItsJobAndRetainsWinners(t *testing.T) {
	m := NewManager(0, t.TempDir())
	a, cancelA := context.WithCancel(context.Background())
	b, cancelB := context.WithCancel(context.Background())
	defer cancelA()
	defer cancelB()
	m.provisionJobs = map[string]context.CancelFunc{"a": cancelA, "b": cancelB}
	m.tunnels[1] = &Tunnel{Slot: 1, Status: "up"}
	for _, tc := range []struct {
		method, id string
		status     int
	}{{"GET", "a", 405}, {"POST", "missing", 409}, {"POST", "a", 202}} {
		w := httptest.NewRecorder()
		apiProvisionStop(m).ServeHTTP(w, httptest.NewRequest(tc.method, "/api/provision/stop?id="+tc.id, nil))
		if w.Code != tc.status {
			t.Fatal("incorrect cancellation response", tc, w.Code)
		}
	}
	if a.Err() == nil || b.Err() != nil || len(m.visibleTunnels()) != 1 {
		t.Fatal("stop canceled another job or removed an existing winner")
	}
	m.cancelProvisions()
	if b.Err() == nil {
		t.Fatal("stop-all left a provisioning job running")
	}
}

func TestProvisionCountsDistinctFreeEndpoints(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.nodes = []Node{
		{HostName: "occupied-alias", CountryCode: "JP", IP: "198.51.100.1"},
		{HostName: "free", CountryCode: "JP", IP: "198.51.100.2"},
		{HostName: "free-alias", CountryCode: "JP", IP: "198.51.100.2"},
		{HostName: "third", CountryCode: "JP", IP: "198.51.100.3"},
	}
	m.tunnels[1] = &Tunnel{Slot: 1, Node: Node{HostName: "occupied", IP: "198.51.100.1"}}
	picks, err := m.pickEveryRegion(3)
	if err != nil || len(picks) != 2 {
		t.Fatal("aliases created impossible country quotas", picks, err)
	}
	regions := m.Regions()
	if len(regions) != 1 || regions[0].Available != 2 {
		t.Fatal("wizard counted occupied or duplicate endpoints", regions)
	}
}

func TestStopAllAPICancelsBackgroundCreationWithNoVisibleExits(t *testing.T) {
	m := NewManager(0, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.provisionJobs = map[string]context.CancelFunc{"pending": cancel}
	w := httptest.NewRecorder()
	apiStopAll(m).ServeHTTP(w, httptest.NewRequest("GET", "/api/stop-all", nil))
	if w.Code != 405 || ctx.Err() != nil {
		t.Fatal("GET canceled a background job")
	}
	w = httptest.NewRecorder()
	apiStopAll(m).ServeHTTP(w, httptest.NewRequest("POST", "/api/stop-all", nil))
	if w.Code != 200 || ctx.Err() == nil {
		t.Fatal("stop-all left invisible candidates running", w.Code)
	}
}
