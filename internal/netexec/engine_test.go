package netexec

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

var injected = errors.New("injected failure")

type memoryRecords struct {
	values     map[string]record
	saves      int
	failSave   int
	failDelete bool
}

func (m *memoryRecords) Load(name string) (record, bool, error) {
	r, ok := m.values[name]
	return r, ok, nil
}
func (m *memoryRecords) Save(name string, r record) error {
	m.saves++
	if m.saves == m.failSave {
		return injected
	}
	m.values[name] = r
	return nil
}
func (m *memoryRecords) Delete(name string) error {
	if m.failDelete {
		return injected
	}
	delete(m.values, name)
	return nil
}
func (m *memoryRecords) Count() (int, error) { return len(m.values), nil }

type fakeBackend struct {
	ids              map[string]Identity
	guarded          map[string]bool
	calls            []string
	serial           uint64
	fail             string
	leaveOnCreate    bool
	busy             bool
	rollbackCanceled bool
}

func (b *fakeBackend) Lookup(ctx context.Context, p Plan) (Identity, bool, error) {
	id, ok := b.ids[p.Namespace]
	return id, ok, nil
}
func (b *fakeBackend) Create(ctx context.Context, p Plan) (Identity, error) {
	b.calls = append(b.calls, "create")
	if b.fail == "create" && !b.leaveOnCreate {
		return Identity{}, injected
	}
	b.serial++
	id := Identity{Device: 4, Inode: b.serial}
	b.ids[p.Namespace] = id
	if b.fail == "create" {
		return Identity{}, injected
	}
	return id, nil
}
func (b *fakeBackend) InstallGuard(ctx context.Context, p Plan, id Identity) error {
	b.calls = append(b.calls, "guard")
	if b.fail == "guard" {
		return injected
	}
	b.guarded[p.Namespace] = true
	return nil
}
func (b *fakeBackend) Verify(ctx context.Context, p Plan, id Identity) (bool, error) {
	b.calls = append(b.calls, "verify")
	if b.fail == "verify" {
		return false, injected
	}
	return b.ids[p.Namespace] == id && b.guarded[p.Namespace], nil
}
func (b *fakeBackend) Delete(ctx context.Context, p Plan, id Identity) error {
	b.calls = append(b.calls, "delete")
	if ctx.Err() != nil {
		b.rollbackCanceled = true
		return ctx.Err()
	}
	if b.ids[p.Namespace] != id {
		return ErrOwnership
	}
	if b.busy {
		return ErrBusy
	}
	if b.fail == "delete" {
		return injected
	}
	delete(b.ids, p.Namespace)
	delete(b.guarded, p.Namespace)
	return nil
}
func fixture(t *testing.T) (*Engine, *memoryRecords, *fakeBackend) {
	t.Helper()
	records := &memoryRecords{values: map[string]record{}}
	b := &fakeBackend{ids: map[string]Identity{}, guarded: map[string]bool{}}
	e, err := newEngine("instance_12345678", records, b)
	if err != nil {
		t.Fatal(err)
	}
	return e, records, b
}

const testExit = "exit_123456789012"

func TestPrepareAndCleanupAreIdempotent(t *testing.T) {
	e, records, b := fixture(t)
	ctx := context.Background()
	status, err := e.Prepare(ctx, testExit)
	if err != nil || status.Phase != "quarantined" || !status.QuarantineVerified || status.TunnelReady || status.ProxyListening {
		t.Fatalf("bad preparation: %+v %v", status, err)
	}
	if !reflect.DeepEqual(b.calls, []string{"create", "guard", "verify"}) {
		t.Fatal(b.calls)
	}
	first := b.ids[status.Plan.Namespace]
	again, err := e.Prepare(ctx, testExit)
	if err != nil || again.Phase != "quarantined" || b.ids[status.Plan.Namespace] != first || b.serial != 1 {
		t.Fatal("prepare recreated the sandbox", err)
	}
	status, err = e.Cleanup(ctx, testExit)
	if err != nil || status.Phase != "absent" || len(records.values) != 0 || len(b.ids) != 0 {
		t.Fatal("cleanup failed", err)
	}
	if _, err = e.Cleanup(ctx, testExit); err != nil {
		t.Fatal(err)
	}
}
func TestPreparationFailureRollsBackOnlyKnownResources(t *testing.T) {
	for _, stage := range []string{"create", "guard", "verify", "save-intent", "save-identity", "save-ready"} {
		t.Run(stage, func(t *testing.T) {
			e, records, b := fixture(t)
			switch stage {
			case "save-intent":
				records.failSave = 1
			case "save-identity":
				records.failSave = 2
			case "save-ready":
				records.failSave = 3
			default:
				b.fail = stage
			}
			status, err := e.Prepare(context.Background(), testExit)
			if err == nil || status.QuarantineVerified || len(b.ids) != 0 || len(records.values) != 0 {
				t.Fatalf("unsafe failed preparation: %+v %v", status, err)
			}
			if stage == "save-intent" && len(b.calls) != 0 {
				t.Fatal("created resources before persistent intent")
			}
		})
	}
}
func TestAmbiguousCreationIsNeverDeletedOrAdopted(t *testing.T) {
	e, records, b := fixture(t)
	b.fail = "create"
	b.leaveOnCreate = true
	status, err := e.Prepare(context.Background(), testExit)
	if err == nil || len(b.ids) != 1 || len(records.values) != 1 {
		t.Fatal("did not preserve ambiguous resources")
	}
	for _, call := range b.calls {
		if call == "delete" {
			t.Fatal("deleted resource of unknown ownership")
		}
	}
	if _, err = e.Cleanup(context.Background(), testExit); !errors.Is(err, ErrOwnership) {
		t.Fatal(err)
	}
	b.fail = ""
	status, err = e.Prepare(context.Background(), testExit)
	if !errors.Is(err, ErrConflict) || status.Phase != "incomplete" {
		t.Fatalf("adopted ambiguous namespace: %+v %v", status, err)
	}
}
func TestOwnershipAndDriftAreNotAutoRepaired(t *testing.T) {
	for _, kind := range []string{"unowned", "replacement", "wrong-record", "drifted", "busy"} {
		t.Run(kind, func(t *testing.T) {
			e, records, b := fixture(t)
			ctx := context.Background()
			status, err := e.Prepare(ctx, testExit)
			if err != nil {
				t.Fatal(err)
			}
			name := status.Plan.Namespace
			switch kind {
			case "unowned":
				delete(records.values, name)
			case "replacement":
				b.ids[name] = Identity{Device: 4, Inode: 999}
			case "wrong-record":
				r := records.values[name]
				r.InstanceID = "different_instance"
				records.values[name] = r
			case "drifted":
				b.guarded[name] = false
			case "busy":
				b.busy = true
			}
			before := len(b.calls)
			status, inspectErr := e.Inspect(ctx, testExit)
			if kind != "busy" && status.QuarantineVerified {
				t.Fatal("claimed protection despite ownership/drift")
			}
			if kind == "wrong-record" && !errors.Is(inspectErr, ErrOwnership) {
				t.Fatal(inspectErr)
			}
			if kind != "busy" {
				if _, err = e.Prepare(ctx, testExit); err == nil {
					t.Fatal("unexpected automatic repair")
				}
				for _, call := range b.calls[before:] {
					if call == "create" || call == "guard" || call == "delete" {
						t.Fatal("mutated conflicting resources")
					}
				}
			}
			if kind == "drifted" {
				if _, err = e.Cleanup(ctx, testExit); err != nil {
					t.Fatal(err)
				}
				return
			}
			if _, err = e.Cleanup(ctx, testExit); err == nil || len(b.ids) != 1 {
				t.Fatal("deleted unowned or busy namespace")
			}
		})
	}
}
func TestRollbackHasIndependentTimeoutAndPreservesFailedCleanup(t *testing.T) {
	e, records, b := fixture(t)
	b.fail = "guard"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.Prepare(ctx, testExit)
	if err == nil || b.rollbackCanceled || len(b.ids) != 0 {
		t.Fatal("cancellation skipped rollback")
	}
	e, records, b = fixture(t)
	b.fail = "guard"
	b.busy = true
	status, err := e.Prepare(context.Background(), testExit)
	if err == nil || status.Phase != "cleanup-required" || len(records.values) != 1 || len(b.ids) != 1 {
		t.Fatal("lost failed rollback state")
	}
	b.fail = ""
	b.busy = false
	if _, err = e.Cleanup(context.Background(), testExit); err != nil {
		t.Fatal(err)
	}
}
func TestSandboxBudgetAndConcurrentPrepare(t *testing.T) {
	e, _, b := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.Prepare(context.Background(), testExit); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if b.serial != 1 {
		t.Fatal("concurrent preparation created duplicates")
	}
	for i := 1; i < MaxSandboxes; i++ {
		if _, err := e.Prepare(context.Background(), fmt.Sprintf("exit_%08d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Prepare(context.Background(), "exit_over_budget"); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}
func TestStaleRecordCleanupDoesNotTouchNamespaces(t *testing.T) {
	e, records, b := fixture(t)
	ctx := context.Background()
	status, err := e.Prepare(ctx, testExit)
	if err != nil {
		t.Fatal(err)
	}
	delete(b.ids, status.Plan.Namespace)
	b.calls = nil
	status, err = e.Inspect(ctx, testExit)
	if err != nil || status.Phase != "stale-record" {
		t.Fatal(status, err)
	}
	if _, err = e.Cleanup(ctx, testExit); err != nil || len(records.values) != 0 || len(b.calls) != 0 {
		t.Fatal("unsafe stale-record cleanup", err)
	}
}
