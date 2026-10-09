//go:build linux

package netexec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLinuxLabRefusesActiveProcess(t *testing.T) {
	e, b := labEngine(t)
	ctx := labContext(t)
	exitID := "lab_busy_process_12345678"
	status, err := e.Prepare(ctx, exitID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { labCleanup(t, e, exitID) })
	identity, _, err := b.Lookup(ctx, status.Plan)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := ownedNamespace(status.Plan, identity)
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	sleep, err := trustedBinary("sleep")
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, b.tools.nsenter, "--net=/proc/self/fd/3", "--", sleep, "30")
	child.ExtraFiles = []*os.File{ns}
	child.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	child.WaitDelay = time.Second
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	finished := false
	finish := func() {
		if finished {
			return
		}
		finished = true
		// Only this test's direct child is terminated; no namespace-wide pkill.
		if err := child.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("stop lab child: %v", err)
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("lab child failed to terminate")
		}
	}
	t.Cleanup(finish)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var stat unix.Stat_t
		err = unix.Stat(fmt.Sprintf("/proc/%d/ns/net", child.Process.Pid), &stat)
		if err == nil && (Identity{Device: uint64(stat.Dev), Inode: stat.Ino}) == identity {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lab child did not enter the intended namespace")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = e.Cleanup(ctx, exitID); !errors.Is(err, ErrBusy) {
		t.Fatalf("active process cleanup was not refused: %v", err)
	}
	if err = child.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("cleanup killed the active process: %v", err)
	}
	if current, err := e.Inspect(ctx, exitID); err != nil || !current.QuarantineVerified {
		t.Fatalf("cleanup damaged quarantine: %+v %v", current, err)
	}
	finish()
	if _, err = e.Cleanup(ctx, exitID); err != nil {
		t.Fatal(err)
	}
	t.Log("Observed: active-process cleanup refused; owned child stayed alive; cleanup succeeded after that child exited.")
}

func TestLinuxLabRestartAndLocks(t *testing.T) {
	e, _ := labEngine(t)
	ctx := labContext(t)
	root := filepath.Dir(e.records.(*diskRecords).directory)
	exitID := "lab_restart_12345678"
	current := e
	t.Cleanup(func() { labCleanup(t, current, exitID); current.Close() })
	first, err := e.Prepare(ctx, exitID)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutating := range []bool{false, true} {
		other, err := openRuntime(e.instanceID, mutating, root)
		if err == nil {
			other.Close()
			t.Fatal("competing helper acquired a held exclusive lock")
		}
		if !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := openRuntime(e.instanceID, false, root)
	if err != nil {
		t.Fatal(err)
	}
	current = reader
	if status, err := reader.Inspect(ctx, exitID); err != nil || !status.QuarantineVerified || status.Plan.Namespace != first.Plan.Namespace {
		t.Fatalf("restart did not recover observed quarantine: %+v %v", status, err)
	}
	if _, err = reader.Prepare(ctx, exitID); err == nil {
		t.Fatal("read-only helper mutated resources")
	}
	if _, err = reader.Cleanup(ctx, exitID); err == nil {
		t.Fatal("read-only helper cleaned resources")
	}
	reader2, err := openRuntime(e.instanceID, false, root)
	if err != nil {
		t.Fatal(err)
	}
	if err = reader2.Close(); err != nil {
		t.Fatal(err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	writer, err := openRuntime(e.instanceID, true, root)
	if err != nil {
		t.Fatal(err)
	}
	current = writer
	if status, err := writer.Prepare(ctx, exitID); err != nil || !status.QuarantineVerified || status.Plan.Namespace != first.Plan.Namespace {
		t.Fatalf("restart recreated or lost quarantine: %+v %v", status, err)
	}
	if _, err = writer.Cleanup(ctx, exitID); err != nil {
		t.Fatal(err)
	}
	t.Log("Observed: exclusive lock blocks competing helpers; shared inspection works; restart retains the same verified namespace without recreation.")
}

func TestLinuxLabRecordObjectSafety(t *testing.T) {
	e, _ := labEngine(t)
	records := e.records.(*diskRecords)
	plan, err := BuildPlan(e.instanceID, "lab_record_12345678")
	if err != nil {
		t.Fatal(err)
	}
	filename, err := records.filename(plan.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"fifo", "symlink", "directory", "hardlink", "permissions", "invalid-json"} {
		t.Run(kind, func(t *testing.T) {
			peer := filepath.Join(records.directory, "test-peer")
			switch kind {
			case "fifo":
				err = unix.Mkfifo(filename, 0600)
			case "symlink":
				if err = os.WriteFile(peer, []byte("must remain untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(peer, filename)
			case "directory":
				err = os.Mkdir(filename, 0700)
			case "hardlink":
				if err = os.WriteFile(peer, []byte("must remain untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				err = os.Link(peer, filename)
			case "permissions":
				if err = os.WriteFile(filename, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
				err = os.Chmod(filename, 0666)
			case "invalid-json":
				err = os.WriteFile(filename, []byte("{}{}"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// Exact, test-created objects only. Directories are empty, not recursive.
				if err := os.Remove(filename); err != nil {
					t.Error(err)
				}
				if kind == "symlink" || kind == "hardlink" {
					if err := os.Remove(peer); err != nil {
						t.Error(err)
					}
				}
			})
			done := make(chan error, 1)
			go func() { _, _, err := records.Load(plan.Namespace); done <- err }()
			select {
			case readErr := <-done:
				if readErr == nil {
					t.Fatal("unsafe record object accepted")
				}
			case <-time.After(time.Second):
				// Recover the specific injected FIFO so a regression cannot hang CI.
				if kind == "fifo" {
					fd, openErr := unix.Open(filename, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
					if openErr == nil {
						_ = unix.Close(fd)
					}
				}
				select {
				case <-done:
				case <-time.After(time.Second):
				}
				t.Fatal("unsafe filesystem object blocked the ownership reader")
			}
			if kind == "symlink" || kind == "hardlink" {
				raw, err := os.ReadFile(peer)
				if err != nil || string(raw) != "must remain untouched" {
					t.Fatal("peer file was changed", err)
				}
			}
		})
	}
	t.Log("Observed: FIFO, symlink, directory, hard link, permissive mode and malformed ownership records were rejected without hanging or touching the peer file.")
}

func TestLinuxLabResourceBudget(t *testing.T) {
	if os.Getenv("MIKU_GATE_NETNS_TEST") != "1" || os.Getenv("MIKU_GATE_LAB_BUDGET") != "1" {
		t.Skip("budget validation requires the opt-in cgroup-limited lab")
	}
	if runtime.GOMAXPROCS(0) != 1 {
		t.Fatal("expected one Go scheduler CPU")
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	var cgroup string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "0::/") {
			cgroup = strings.TrimPrefix(line, "0::")
			break
		}
	}
	if cgroup == "" || filepath.Clean(cgroup) != cgroup || strings.Contains(cgroup, "..") {
		t.Fatal("expected canonical cgroup v2 path")
	}
	directory := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(cgroup, "/"))
	limit := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	memory, err := strconv.ParseUint(limit("memory.max"), 10, 64)
	if err != nil || memory == 0 || memory > 512<<20 {
		t.Fatalf("missing effective 512 MiB memory cap: %d %v", memory, err)
	}
	cpu := strings.Fields(limit("cpu.max"))
	if len(cpu) != 2 {
		t.Fatal("invalid cpu.max")
	}
	quota, qerr := strconv.ParseUint(cpu[0], 10, 64)
	period, perr := strconv.ParseUint(cpu[1], 10, 64)
	if qerr != nil || perr != nil || quota == 0 || period == 0 || quota > period {
		t.Fatal("missing effective single-CPU quota")
	}
	tasks, err := strconv.ParseUint(limit("pids.max"), 10, 64)
	if err != nil || tasks == 0 || tasks > 64 {
		t.Fatal("missing effective process/task limit")
	}
	if limit("memory.swap.max") != "0" {
		t.Fatal("swap was not disabled for the bounded lab")
	}
	t.Logf("Effective cgroup limits: memory.max=%d bytes, cpu.max=%s, pids.max=%d, memory.swap.max=0, GOMAXPROCS=1; memory.peak=%s bytes. This is not VPS performance validation.", memory, strings.Join(cpu, " "), tasks, limit("memory.peak"))
}
