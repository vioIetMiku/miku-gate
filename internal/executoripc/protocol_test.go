package executoripc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"mikugate/internal/netexec"
)

const testInstance = "instance_ipc_12345678"
const testExit = "exit_ipc_12345678"
const testID = "0123456789abcdef0123456789abcdef"

type fakeRuntime struct {
	mu      sync.Mutex
	calls   int
	phase   string
	err     error
	started chan struct{}
	wait    bool
	lie     bool
}

func (f *fakeRuntime) run(ctx context.Context, id, operation string) (netexec.Result, error) {
	f.mu.Lock()
	f.calls++
	phase := f.phase
	err := f.err
	lie := f.lie
	wait := f.wait
	started := f.started
	if operation == "prepare" {
		phase = "quarantined"
		f.phase = phase
	}
	if operation == "cleanup" {
		phase = "absent"
		f.phase = phase
	}
	f.mu.Unlock()
	if wait {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return netexec.Result{}, ctx.Err()
	}
	if err != nil {
		return netexec.Result{}, err
	}
	if phase == "" {
		phase = "absent"
	}
	plan, _ := netexec.BuildPlan(testInstance, id)
	return netexec.Result{Plan: plan, Phase: phase, QuarantineVerified: phase == "quarantined", TunnelReady: lie}, nil
}
func (f *fakeRuntime) Inspect(c context.Context, id string) (netexec.Result, error) {
	return f.run(c, id, "inspect")
}
func (f *fakeRuntime) Prepare(c context.Context, id string) (netexec.Result, error) {
	return f.run(c, id, "prepare")
}
func (f *fakeRuntime) Cleanup(c context.Context, id string) (netexec.Result, error) {
	return f.run(c, id, "cleanup")
}
func testClient(t *testing.T, s *service) *Client {
	t.Helper()
	return &Client{instance: testInstance, dial: func(ctx context.Context, instance string) (net.Conn, error) {
		client, server := net.Pipe()
		go s.handle(ctx, server)
		return client, nil
	}}
}
func TestProtocolAllowedOperationsAndBounds(t *testing.T) {
	valid := []Request{{Version: 1, ID: testID, Operation: "capabilities"}, {Version: 1, ID: testID, Operation: "inspect", ExitID: testExit}, {Version: 1, ID: testID, Operation: "prepare", ExitID: testExit, Apply: true}, {Version: 1, ID: testID, Operation: "cleanup", ExitID: testExit, Apply: true}}
	for _, request := range valid {
		var b bytes.Buffer
		if err := writeFrame(&b, request, MaxRequestBytes); err != nil {
			t.Fatal(err)
		}
		raw, err := readFrame(&b, MaxRequestBytes)
		if err != nil {
			t.Fatal(err)
		}
		var got Request
		if decode(raw, &got) != nil || got.validate() != nil || got != request {
			t.Fatal("valid round-trip rejected")
		}
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Version = 2 }, func(r *Request) { r.ID = "../bad" }, func(r *Request) { r.Operation = "exec" }, func(r *Request) { r.Apply = false }, func(r *Request) { r.ExitID = "../../etc" }, func(r *Request) { r.ExitID = strings.Repeat("a", 65) },
	} {
		r := valid[2]
		mutate(&r)
		if r.validate() == nil {
			t.Fatal("unsafe mutation accepted")
		}
	}
	for _, request := range []Request{{Version: 1, ID: testID, Operation: "inspect", ExitID: testExit, Apply: true}, {Version: 1, ID: testID, Operation: "capabilities", ExitID: testExit}, {Version: 1, ID: testID, Operation: "capabilities", Apply: true}} {
		if request.validate() == nil {
			t.Fatal("invalid read-only request accepted")
		}
	}
	for _, size := range []uint32{0, 1, MaxRequestBytes + 1, 0xffffffff} {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, size)
		if _, err := readFrame(&b, MaxRequestBytes); err == nil {
			t.Fatal("invalid frame length accepted")
		}
	}
}
func TestStrictJSONRejectsAmbiguityAndUnexpectedFields(t *testing.T) {
	for _, raw := range []string{
		"null", "[]", "{}{}", `{"version":1,"id":"` + testID + `","operation":"inspect","operation":"prepare"}`,
		`{"version":1,"id":"` + testID + `","operation":"capabilities","command":"id"}`,
		`{"version":1,"id":"` + testID + `","operation":"capabilities","instance_id":"other_instance"}`,
		`{"version":1,"id":"` + testID + `","operation":"capabilities","path":"/etc/shadow"}`,
		strings.Repeat(`{"nest":`, 20) + "0" + strings.Repeat("}", 20),
	} {
		var request Request
		if decode([]byte(raw), &request) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
func TestClientServiceLifecycleRemainsSandboxOnly(t *testing.T) {
	f := &fakeRuntime{}
	client := testClient(t, newService(testInstance, f))
	caps, err := client.Capabilities(t.Context())
	if err != nil || !caps.SandboxAvailable || caps.VPNAvailable || caps.ProxyAvailable {
		t.Fatal(caps, err)
	}
	state, err := client.Inspect(t.Context(), testExit)
	if err != nil || state.Phase != "absent" {
		t.Fatal(state, err)
	}
	state, err = client.Prepare(t.Context(), testExit)
	if err != nil || !state.QuarantineVerified || state.TunnelReady || state.ProxyListening {
		t.Fatal(state, err)
	}
	state, err = client.Cleanup(t.Context(), testExit)
	if err != nil || state.Phase != "absent" {
		t.Fatal(state, err)
	}
	if f.calls != 3 {
		t.Fatal("capabilities touched the runtime")
	}
}
func TestNoArbitraryErrorsOrFakeTunnelState(t *testing.T) {
	for _, injected := range []error{errors.New("secret external command output"), netexec.ErrOwnership, netexec.ErrBusy, netexec.ErrLimit} {
		f := &fakeRuntime{err: injected}
		client := testClient(t, newService(testInstance, f))
		_, err := client.Prepare(t.Context(), testExit)
		var rpc *RPCError
		if !errors.As(err, &rpc) || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe error forwarding", err)
		}
	}
	f := &fakeRuntime{lie: true}
	client := testClient(t, newService(testInstance, f))
	_, err := client.Prepare(t.Context(), testExit)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != "unverified" {
		t.Fatal("fake active tunnel reported", err)
	}
}
func TestConcurrentOperationsRejectInsteadOfQueue(t *testing.T) {
	f := &fakeRuntime{wait: true, started: make(chan struct{}, 1)}
	s := newService(testInstance, f)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan response, 1)
	request := Request{Version: 1, ID: testID, Operation: "inspect", ExitID: testExit}
	go func() { done <- s.dispatch(ctx, request) }()
	select {
	case <-f.started:
	case <-time.After(time.Second):
		t.Fatal("request never began")
	}
	if out := s.dispatch(t.Context(), request); out.Error == nil || out.Error.Code != "busy" {
		t.Fatal("operation was queued")
	}
	if out := s.dispatch(t.Context(), Request{Version: 1, ID: testID, Operation: "capabilities"}); out.Error != nil || out.Capabilities == nil {
		t.Fatal("capability query unnecessarily blocked")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("operation ignored cancellation")
	}
}
func TestDisconnectCancelsOperation(t *testing.T) {
	f := &fakeRuntime{wait: true, started: make(chan struct{}, 1)}
	s := newService(testInstance, f)
	client, server := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() { s.handle(t.Context(), server); close(done) }()
	if err := writeFrame(client, Request{Version: 1, ID: testID, Operation: "prepare", ExitID: testExit, Apply: true}, MaxRequestBytes); err != nil {
		t.Fatal(err)
	}
	<-f.started
	client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disconnect left an operation alive")
	}
}
func TestClientRejectsUnexpectedReply(t *testing.T) {
	for _, kind := range []string{"nonce", "version", "instance", "vpn", "unknown-error", "result-mixed"} {
		t.Run(kind, func(t *testing.T) {
			client := &Client{instance: testInstance, dial: func(ctx context.Context, instance string) (net.Conn, error) {
				a, b := net.Pipe()
				go func() {
					defer b.Close()
					data, _ := readFrame(b, MaxRequestBytes)
					var request Request
					_ = json.Unmarshal(data, &request)
					caps := capabilities(instance)
					out := response{Version: Version, ID: request.ID, Capabilities: &caps}
					switch kind {
					case "nonce":
						out.ID = testID
					case "version":
						out.Version = 7
					case "instance":
						caps.InstanceID = "other_instance"
					case "vpn":
						caps.VPNAvailable = true
					case "unknown-error":
						out.Capabilities = nil
						out.Error = &RPCError{Code: "secret", Message: "untrusted"}
					case "result-mixed":
						out.Result = &netexec.Result{}
					}
					_ = writeFrame(b, out, MaxResponseBytes)
				}()
				return a, nil
			}}
			if _, err := client.Capabilities(t.Context()); !errors.Is(err, ErrProtocol) {
				t.Fatal("untrusted reply accepted", err)
			}
		})
	}
}
func FuzzRequestDecoder(f *testing.F) {
	f.Add([]byte(`{"version":1,"id":"0123456789abcdef0123456789abcdef","operation":"capabilities"}`))
	f.Add([]byte("null"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > MaxRequestBytes {
			return
		}
		var request Request
		if decode(raw, &request) == nil && request.validate() == nil {
			if request.Operation != "capabilities" && netexec.ValidateID(request.ExitID) != nil {
				t.Fatal("invalid resource id accepted")
			}
		}
	})
}
