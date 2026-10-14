package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// inMainNetns 里跑的东西必须落在母机的网络命名空间。
//
// 注意读的是 /proc/thread-self 而不是 /proc/self：命名空间是**线程级**的，
// 读进程级的那个看不出线程有没有被切走——这正是当初没发现问题的原因。
func TestInMainNetnsRunsInMainNamespace(t *testing.T) {
	if err := initMainNetns(); err != nil {
		t.Skipf("拿不到网络命名空间，跳过: %v", err)
	}
	want, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Skipf("读不了命名空间，跳过: %v", err)
	}

	var got string
	if err := inMainNetns(func() error {
		var e error
		got, e = os.Readlink("/proc/thread-self/ns/net")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("跑在了别的命名空间里: %s，应当是 %s", got, want)
	}
}

// 线程已经被切进别的命名空间时，inMainNetns 要能把它拽回来。
//
// 这是事故的核心场景：dialerInNetns 把线程带进隧道，
// runtime 又从这条线程 clone 出新线程去跑别的活，于是 fork 出的子进程
// 继承了隧道的命名空间。
//
// 必须放到子进程里跑。这个用例会 Unshare 出一个空命名空间，
// runtime 一旦从那条线程 clone 出新线程，整个测试进程的线程池就被污染了——
// 后面所有 net.Listen / net.Dial 都会报 "network is unreachable"。
// 第一版没隔离，结果把同一批里另外四个用例全搞挂了，
// 那次失败本身就是这个 bug 的现场复现。
func TestInMainNetnsRecoversFromForeignNamespace(t *testing.T) {
	if os.Getenv("FANOUT_NETNS_CHILD") != "1" {
		if os.Geteuid() != 0 {
			t.Skip("要 root 才能建命名空间")
		}
		cmd := exec.Command(os.Args[0],
			"-test.run=^TestInMainNetnsRecoversFromForeignNamespace$", "-test.v")
		cmd.Env = append(os.Environ(), "FANOUT_NETNS_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("子进程里的用例失败: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "PASS") {
			t.Fatalf("子进程没跑过:\n%s", out)
		}
		return
	}

	if err := initMainNetns(); err != nil {
		t.Skipf("拿不到网络命名空间，跳过: %v", err)
	}
	main, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Skip(err)
	}

	done := make(chan string, 1)
	fail := make(chan error, 1)
	go func() {
		// 故意把这条线程切进一个新命名空间，模拟被 dialerInNetns 污染
		runtime.LockOSThread()
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			fail <- err
			return
		}
		cur, _ := os.Readlink("/proc/thread-self/ns/net")
		if cur == main {
			fail <- nil // 没切成功，这个用例就没意义了
			return
		}
		// 此时从这条线程调 inMainNetns，必须回到母机命名空间
		var got string
		if err := inMainNetns(func() error {
			var e error
			got, e = os.Readlink("/proc/thread-self/ns/net")
			return e
		}); err != nil {
			fail <- err
			return
		}
		done <- got
	}()

	select {
	case err := <-fail:
		t.Skipf("环境不支持这个用例: %v", err)
	case got := <-done:
		if got != main {
			t.Fatalf("没能切回母机命名空间: %s，应当是 %s", got, main)
		}
	}
}

// mainNetns 还没初始化时不能把命令吞掉，要原样执行。
func TestInMainNetnsFallsBackWhenUninitialized(t *testing.T) {
	prev := mainNetns
	mainNetns = nil
	t.Cleanup(func() { mainNetns = prev })

	called := false
	if err := inMainNetns(func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("没初始化时也要照常执行")
	}
}

// 几个包装函数的基本行为：成败、输出都要正确传递。
func TestCmdHelpers(t *testing.T) {
	if err := initMainNetns(); err != nil {
		t.Skipf("拿不到网络命名空间，跳过: %v", err)
	}

	out, err := cmdOutput(exec.Command("echo", "母机"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "母机" {
		t.Fatalf("输出没传出来: %q", out)
	}

	if err := cmdRun(exec.Command("true")); err != nil {
		t.Fatalf("true 不该失败: %v", err)
	}
	if err := cmdRun(exec.Command("false")); err == nil {
		t.Fatal("false 应当返回错误")
	}

	combined, err := cmdCombined(exec.Command("sh", "-c", "echo out; echo err 1>&2"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(combined)
	if !strings.Contains(s, "out") || !strings.Contains(s, "err") {
		t.Fatalf("合并输出不完整: %q", s)
	}

	cmd := exec.Command("sleep", "0.1")
	if err := cmdStart(cmd); err != nil {
		t.Fatal(err)
	}
	if cmd.Process == nil {
		t.Fatal("子进程没起来")
	}
	_ = cmd.Wait()
}

// 连续用不能把线程池搞坏：切换失败的线程不回收会拖垮长跑的进程。
func TestInMainNetnsRepeated(t *testing.T) {
	if err := initMainNetns(); err != nil {
		t.Skipf("拿不到网络命名空间，跳过: %v", err)
	}
	want, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Skip(err)
	}
	for i := 0; i < 50; i++ {
		var got string
		if err := inMainNetns(func() error {
			var e error
			got, e = os.Readlink("/proc/thread-self/ns/net")
			return e
		}); err != nil {
			t.Fatalf("第 %d 次失败: %v", i, err)
		}
		if got != want {
			t.Fatalf("第 %d 次跑错了命名空间: %s", i, got)
		}
	}
}
