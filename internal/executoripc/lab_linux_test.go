//go:build linux

package executoripc

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mikugate/internal/labexec"
	"mikugate/internal/netexec"
)

func requireLab(t *testing.T) string {
	t.Helper()
	if os.Getenv("MIKU_GATE_NETNS_TEST") != "1" {
		t.Skip("Linux IPC integration is opt-in: MIKU_GATE_NETNS_TEST=1")
	}
	if os.Geteuid() != 0 {
		t.Fatal("opted-in IPC integration requires root")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	return "ipc_lab_" + hex.EncodeToString(nonce[:])
}
func removeEmptyLease(t *testing.T, instance string) {
	t.Helper()
	socket, _ := SocketPath(instance)
	directory := filepath.Dir(socket)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Error(err)
		return
	}
	if len(entries) != 1 || entries[0].Name() != ".lock" {
		t.Errorf("unexpected IPC records retained: %s", directory)
		return
	}
	if err = os.Remove(filepath.Join(directory, ".lock")); err != nil {
		t.Error(err)
		return
	}
	if err = os.Remove(directory); err != nil {
		t.Error(err)
	}
}
func removeEmptyEngine(t *testing.T, instance string) {
	t.Helper()
	sum := sha256.Sum256([]byte(instance))
	directory := filepath.Join(netexec.RuntimeDir, hex.EncodeToString(sum[:16]))
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Error(err)
		return
	}
	if len(entries) != 1 || entries[0].Name() != ".lock" {
		t.Errorf("namespace record retained for review: %s", directory)
		return
	}
	if err = os.Remove(filepath.Join(directory, ".lock")); err != nil {
		t.Error(err)
		return
	}
	if err = os.Remove(directory); err != nil {
		t.Error(err)
	}
}
func startLabDaemon(t *testing.T, instance string) (func(), string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, instance, 65534, func(path string) { ready <- path }) }()
	var socket string
	select {
	case socket = <-ready:
	case err := <-done:
		cancel()
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("executor did not become ready")
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(12 * time.Second):
				t.Error("executor did not stop within rollback budget")
			}
		})
	}
	t.Cleanup(stop)
	return stop, socket
}
func helperCommand(t *testing.T, binary, instance, mode string, uid uint32) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestIPCClientHelper$", "-test.v", "-test.timeout=35s")
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB", "MIKU_IPC_CHILD=" + mode, "MIKU_IPC_INSTANCE=" + instance}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: 65534, Groups: []uint32{65534}}, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = time.Second
	return cmd
}
func runHelper(t *testing.T, binary, instance, mode string, uid uint32) {
	t.Helper()
	out, err := helperCommand(t, binary, instance, mode, uid).CombinedOutput()
	if err != nil {
		t.Fatalf("child UID %d mode %s failed: %v\n%s", uid, mode, err, out)
	}
	if !strings.Contains(string(out), "IPC_CLIENT_OK") {
		t.Fatalf("missing child result: %s", out)
	}
}

func TestLinuxIPCPeerCredentialsLifecycle(t *testing.T) {
	instance := requireLab(t)
	t.Cleanup(func() { removeEmptyLease(t, instance); removeEmptyEngine(t, instance) })
	binary := labexec.CopySelf(t)
	stop, socket := startLabDaemon(t, instance)
	identity, err := checkSocket(socket)
	if err != nil || identity.Inode == 0 {
		t.Fatal("unsafe socket", err)
	}
	runHelper(t, binary, instance, "lifecycle", 65534)
	runHelper(t, binary, instance, "denied", 65533) // Same primary group, different UID.
	client, err := NewClient(instance)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Capabilities(t.Context()); err == nil {
		t.Fatal("root caller bypassed explicit allowed UID")
	}
	runHelper(t, binary, instance, "malformed", 65534)
	runHelper(t, binary, instance, "saturate", 65534)
	runHelper(t, binary, instance, "prepare-only", 65534)
	stop()
	if _, err = os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("socket not cleaned on shutdown")
	}
	// Daemon exit preserves the kernel guard; an explicitly authorized restart
	// recovers the same resource and can clean it after re-verifying identity.
	stopAgain, _ := startLabDaemon(t, instance)
	runHelper(t, binary, instance, "recover-cleanup", 65534)
	stopAgain()
	t.Log("Observed: cross-UID client created/inspected/cleaned a real quarantine; same-group wrong UID and root rejected; malformed commands and excess idle connections rejected; daemon restart retained and recovered kernel state.")
}
func TestLinuxIPCStaleSocketOwnership(t *testing.T) {
	instance := requireLab(t)
	t.Cleanup(func() { removeEmptyLease(t, instance) })
	p, err := makePolicy(instance, 65534)
	if err != nil {
		t.Fatal(err)
	}
	first, err := openLease(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = openLease(p); err == nil {
		t.Fatal("second daemon obtained socket lock")
	}
	// Close descriptors without unlinking, as after an abrupt process exit.
	first.listener.Close()
	first.lock.Close()
	recovered, err := openLease(p)
	if err != nil {
		t.Fatalf("owned dead socket could not be recovered: %v", err)
	}
	if err = recovered.close(); err != nil {
		t.Fatal(err)
	}
	// A socket with no owner record must not be removed or adopted.
	raw, err := net.ListenUnix("unix", &net.UnixAddr{Name: first.path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	raw.SetUnlinkOnClose(false)
	if err = os.Chmod(first.path, 0660); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		raw.Close()
		if err := os.Remove(first.path); err != nil {
			t.Error(err)
		}
	})
	if _, err = openLease(p); !errors.Is(err, ErrPeer) {
		t.Fatalf("unowned socket not refused: %v", err)
	}
	t.Log("Observed: owned stale socket recovered under lock; live/unowned socket retained and refused.")
}
func TestLinuxIPCClientRejectsNonRootPeer(t *testing.T) {
	instance := requireLab(t)
	binary := labexec.CopySelf(t)
	socket, _ := SocketPath(instance)
	directory := filepath.Dir(socket)
	if err := createDirectory(SocketRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(directory, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error(err)
		}
		if err := os.Remove(directory); err != nil {
			t.Error(err)
		}
	})
	cmd := helperCommand(t, binary, instance, "fake-server", 65534)
	pipe, err := cmd.StdoutPipe()
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
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	scanner := bufio.NewScanner(pipe)
	ready := false
	for scanner.Scan() {
		if scanner.Text() == "FAKE_READY" {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("fake server did not listen")
	}
	// Even a root-owned path is insufficient: the accepting socket was created by
	// a non-root process, and SO_PEERCRED must prevent sending it any request.
	if err = os.Chown(directory, 0, 65534); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(directory, 0750); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(socket, 0, 65534); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(socket, 0660); err != nil {
		t.Fatal(err)
	}
	client, _ := NewClient(instance)
	if _, err = client.Capabilities(t.Context()); !errors.Is(err, ErrPeer) {
		t.Fatalf("unprivileged server was trusted: %v", err)
	}
	for scanner.Scan() {
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatal("fake server observed unexpected request bytes", err)
	}
	t.Log("Observed: client rejected a non-root peer before transmitting a framed request, despite a root-owned socket path.")
}

func TestIPCClientHelper(t *testing.T) {
	mode := os.Getenv("MIKU_IPC_CHILD")
	if mode == "" {
		t.Skip("subprocess helper only")
	}
	instance := os.Getenv("MIKU_IPC_INSTANCE")
	socket, err := SocketPath(instance)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "fake-server" {
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		listener.SetUnlinkOnClose(false)
		defer listener.Close()
		fmt.Println("FAKE_READY")
		_ = listener.SetDeadline(time.Now().Add(5 * time.Second))
		conn, err := listener.AcceptUnix()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var data [1]byte
		n, err := conn.Read(data[:])
		if n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("received data from client: %d %v", n, err)
		}
		return
	}
	client, err := NewClient(instance)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	exit := "ipc_lab_exit_12345678"
	if mode == "denied" {
		if _, err = client.Capabilities(ctx); err == nil {
			t.Fatal("wrong UID authorized")
		}
		fmt.Println("IPC_CLIENT_OK")
		return
	}
	if mode == "saturate" {
		var held []net.Conn
		defer func() {
			for _, c := range held {
				c.Close()
			}
		}()
		for i := 0; i < MaxConnections; i++ {
			conn, err := net.DialTimeout("unix", socket, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, conn)
			_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
			if _, err = conn.Write([]byte{0, 0}); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(20 * time.Millisecond)
		extra, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = extra.SetDeadline(time.Now().Add(time.Second))
		_, err = readFrame(extra, MaxResponseBytes)
		extra.Close()
		if err == nil {
			t.Fatal("excess idle connection accepted")
		}
		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			t.Fatal("connection limit queued an excess idle peer")
		}
		for _, conn := range held {
			conn.Close()
		}
		held = nil
		deadline := time.Now().Add(time.Second)
		for {
			if _, err = client.Capabilities(ctx); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("connection slots did not recover", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Println("IPC_CLIENT_OK")
		return
	}
	if mode == "malformed" {
		conn, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		request := map[string]any{"version": 1, "id": testID, "operation": "prepare", "exit_id": exit, "apply": true, "command": "must-not-execute"}
		if err = writeFrame(conn, request, MaxRequestBytes); err != nil {
			t.Fatal(err)
		}
		raw, err := readFrame(conn, MaxResponseBytes)
		if err != nil {
			t.Fatal(err)
		}
		var reply response
		if decode(raw, &reply) != nil || reply.Error == nil || reply.Error.Code != "invalid_request" {
			t.Fatal("arbitrary command field accepted")
		}
		status, err := client.Inspect(ctx, exit)
		if err != nil || status.Phase != "absent" {
			t.Fatal("malformed request created a sandbox", err)
		}
		fmt.Println("IPC_CLIENT_OK")
		return
	}
	caps, err := client.Capabilities(ctx)
	if err != nil || caps.VPNAvailable || caps.ProxyAvailable || !caps.SandboxAvailable {
		t.Fatal("invalid capabilities", err)
	}
	if mode == "recover-cleanup" {
		status, err := client.Inspect(ctx, exit)
		if err != nil || !status.QuarantineVerified {
			t.Fatal("daemon did not recover quarantine", err)
		}
	} else {
		status, err := client.Inspect(ctx, exit)
		if err != nil || status.Phase != "absent" {
			t.Fatal("unexpected pre-existing sandbox", err)
		}
		status, err = client.Prepare(ctx, exit)
		if err != nil || !status.QuarantineVerified || status.TunnelReady || status.ProxyListening {
			t.Fatal("invalid prepare result", err)
		}
		if mode == "prepare-only" {
			fmt.Println("IPC_CLIENT_OK")
			return
		}
	}
	status, err := client.Cleanup(ctx, exit)
	if err != nil || status.Phase != "absent" {
		t.Fatal("cleanup failed", err)
	}
	fmt.Println("IPC_CLIENT_OK uid=" + strconv.Itoa(os.Geteuid()))
}
