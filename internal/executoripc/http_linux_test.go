//go:build linux

package executoripc_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mikugate/internal/executoripc"
	"mikugate/internal/httpapi"
	"mikugate/internal/labexec"
	"mikugate/internal/netexec"
	"mikugate/internal/store"
)

// This goes through actual HTTP authentication/CSRF, an ordinary-user process,
// the real Unix socket, and a separate root daemon. No runtime backend is mocked.
func TestLinuxIPCHTTPControlPlane(t *testing.T) {
	if os.Getenv("MIKU_GATE_NETNS_TEST") != "1" {
		t.Skip("real cross-UID HTTP integration is opt-in")
	}
	if os.Geteuid() != 0 {
		t.Fatal("root test coordinator required")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	base := "/run/miku-gate-ipc-http-tests"
	if err := os.Mkdir(base, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		t.Fatal(err)
	}
	directory := filepath.Join(base, hex.EncodeToString(nonce[:]))
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(directory, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(data, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Remove only these explicitly test-created files; never recursively delete
		// an unknown runtime directory or use a wildcard on namespace objects.
		for _, name := range []string{"test.db-wal", "test.db-shm", "test.db"} {
			if err := os.Remove(filepath.Join(data, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Error(err)
			}
		}
		if err := os.Remove(data); err != nil {
			t.Error(err)
		}
		if err := os.Remove(directory); err != nil {
			t.Error(err)
		}
	})
	binary := labexec.CopySelf(t)
	var err error
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestHTTPIPCChild$", "-test.v", "-test.timeout=45s")
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "GOMAXPROCS=1", "GOMEMLIMIT=192MiB", "MIKU_IPC_HTTP_DATA=" + data}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{65534}}, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	scanner := bufio.NewScanner(stdout)
	instance := ""
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "INSTANCE ") {
			instance = strings.TrimPrefix(scanner.Text(), "INSTANCE ")
			break
		}
	}
	if netexec.ValidateID(instance) != nil {
		t.Fatal("child did not initialize a valid instance identity")
	}
	daemonCtx, stop := context.WithCancel(ctx)
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { done <- executoripc.Serve(daemonCtx, instance, 65534, func(string) { ready <- struct{}{} }) }()
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			stop()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(12 * time.Second):
				t.Error("daemon shutdown timed out")
			}
		}
		socket, _ := executoripc.SocketPath(instance)
		ipcDir := filepath.Dir(socket)
		sum := sha256.Sum256([]byte(instance))
		engineDir := filepath.Join(netexec.RuntimeDir, hex.EncodeToString(sum[:16]))
		for _, dir := range []string{ipcDir, engineDir} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Error(err)
				continue
			}
			if len(entries) != 1 || entries[0].Name() != ".lock" {
				t.Errorf("unexpected runtime resources retained: %s", dir)
				continue
			}
			if err = os.Remove(filepath.Join(dir, ".lock")); err != nil {
				t.Error(err)
				continue
			}
			if err = os.Remove(dir); err != nil {
				t.Error(err)
			}
		}
	})
	select {
	case <-ready:
	case err := <-done:
		stopped = true
		stop()
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err = fmt.Fprintln(stdin, "GO"); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	passed := false
	var output strings.Builder
	for scanner.Scan() {
		output.WriteString(scanner.Text() + "\n")
		if scanner.Text() == "HTTP_IPC_OK" {
			passed = true
		}
	}
	err = cmd.Wait()
	waited = true
	if err != nil || !passed {
		t.Fatalf("real unprivileged HTTP/IPC flow failed: %v\n%s", err, output.String())
	}
	stop()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	stopped = true
	t.Log("Observed: UID 65534 HTTP control server -> root IPC daemon -> kernel quarantine, CSRF/confirmation required, prepare verified, VPN start still 409, deletion interlock, cleanup and safe deletion.")
}
func TestHTTPIPCChild(t *testing.T) {
	data := os.Getenv("MIKU_IPC_HTTP_DATA")
	if data == "" {
		t.Skip("HTTP/IPC child only")
	}
	if os.Geteuid() == 0 {
		t.Fatal("control-plane child must be unprivileged")
	}
	s, err := store.Open(filepath.Join(data, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.InitAccount("admin", "test-session-only"); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateSession("admin", "isolated-cookie", "isolated-csrf", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	instance, err := s.InstanceID()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("INSTANCE " + instance)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() || scanner.Text() != "GO" {
		t.Fatal("missing daemon readiness signal")
	}
	ipc, err := executoripc.NewClient(instance)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := httpapi.NewWithOptions(s, nil, httpapi.Options{Sandbox: ipc})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 35 * time.Second}
	request := func(method, path, body string, csrf bool) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+"/api"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "miku_session", Value: "isolated-cookie"})
		if csrf {
			req.Header.Set("X-CSRF-Token", "isolated-csrf")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, raw
	}
	if status, _ := request("GET", "/executor", "", false); status != 200 {
		t.Fatal(status)
	}
	status, raw := request("POST", "/exits", `{"name":"IPC kernel lab","country":"JP"}`, true)
	if status != 201 {
		t.Fatalf("create exit: %d", status)
	}
	var created struct {
		Exit struct {
			ID string `json:"id"`
		} `json:"exit"`
	}
	if json.Unmarshal(raw, &created) != nil || created.Exit.ID == "" {
		t.Fatal("missing exit identity")
	}
	base := "/exits/" + created.Exit.ID
	if status, _ = request("POST", base+"/sandbox/prepare", `{"confirm":true}`, false); status != 403 {
		t.Fatal("CSRF bypass", status)
	}
	if status, _ = request("POST", base+"/sandbox/prepare", "{}", true); status != 400 {
		t.Fatal("confirmation bypass", status)
	}
	status, raw = request("POST", base+"/sandbox/prepare", `{"confirm":true}`, true)
	var result netexec.Result
	if status != 200 || json.Unmarshal(raw, &result) != nil || !result.QuarantineVerified || result.TunnelReady || result.ProxyListening {
		t.Fatalf("real IPC prepare not verified: %d %s", status, raw)
	}
	if status, _ = request("POST", base+"/start", "", true); status != 409 {
		t.Fatal("IPC enabled fake VPN", status)
	}
	if status, _ = request("DELETE", base, "", true); status != 409 {
		t.Fatal("deleted live sandbox configuration", status)
	}
	if status, raw = request("POST", base+"/sandbox/cleanup", `{"confirm":true}`, true); status != 200 {
		t.Fatalf("cleanup: %d %s", status, raw)
	}
	if status, _ = request("DELETE", base, "", true); status != 204 {
		t.Fatal("delete after cleanup failed", status)
	}
	fmt.Println("HTTP_IPC_OK")
}
