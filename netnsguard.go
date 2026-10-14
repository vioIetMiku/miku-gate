package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"golang.org/x/sys/unix"
)

// 子进程必须从母机的网络命名空间里起，否则会掉进某条隧道里。
//
// 事故经过：Xray 进程活得好好的、配置校验通过、日志干净，但它的入站端口
// 在母机上一个都看不到——因为整个进程被拉进了隧道 fo12 的命名空间，
// 端口只在那条隧道内部监听。客户端连不上，而所有常规排查手段都显示正常。
//
// 成因：dialerInNetns 为了让出站走隧道，会把当前线程 Setns 进隧道的命名空间。
// 虽然它用 LockOSThread 锁住了自己那条线程，但**Go runtime 在这条线程阻塞于
// 系统调用时，会从它 clone 出新线程**去跑别的 goroutine，
// 而 clone 出来的线程继承了隧道的命名空间、并且没有被锁定。
// 之后任何在这种线程上执行的 fork/exec，子进程就继承了错误的命名空间。
// VPN Gate 节点握手慢，正好长期占着线程，把这个窗口放得很大。
//
// 所以凡是"在当前命名空间里生效"的命令——建 veth、改 iptables、开 sysctl、
// 探母机公网 IP、拉起 Xray——都必须显式切回母机命名空间再执行。
// 至于 ip netns exec 那一类，它们自己会切进去，包一层也不影响结果。

// mainNetns 是 fanout 启动时所在的网络命名空间，全程持有不关闭。
var mainNetns *os.File

// initMainNetns 记下母机的网络命名空间。必须在建任何隧道之前调用。
func initMainNetns() error {
	f, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return fmt.Errorf("记录母机网络命名空间失败: %w", err)
	}
	mainNetns = f
	return nil
}

// inMainNetns 把 fn 放到母机网络命名空间里执行。
//
// 单独开一条 goroutine 并锁定线程，免得 Setns 影响到别的活儿。
// 切换失败时不解锁，让这条线程随 goroutine 一起结束，不放回池子重用。
func inMainNetns(fn func() error) error {
	if mainNetns == nil {
		return fn()
	}
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.Setns(int(mainNetns.Fd()), unix.CLONE_NEWNET); err != nil {
			errc <- fmt.Errorf("切回母机网络命名空间失败: %w", err)
			return
		}
		err := fn()
		// 线程现在就在母机命名空间里，和新线程没区别，可以安全放回池子
		runtime.UnlockOSThread()
		errc <- err
	}()
	return <-errc
}

// cmdRun 在母机命名空间里执行命令，只关心成败。
func cmdRun(cmd *exec.Cmd) error {
	return inMainNetns(cmd.Run)
}

// cmdStart 在母机命名空间里拉起一个常驻子进程。
//
// 这是本次事故的修复点：Xray 必须从这里起，否则它的监听端口
// 可能落到某条隧道内部，母机上压根不存在。
func cmdStart(cmd *exec.Cmd) error {
	return inMainNetns(cmd.Start)
}

// cmdOutput 在母机命名空间里执行命令并取标准输出。
func cmdOutput(cmd *exec.Cmd) ([]byte, error) {
	var out []byte
	err := inMainNetns(func() error {
		var e error
		out, e = cmd.Output()
		return e
	})
	return out, err
}

// cmdCombined 在母机命名空间里执行命令并取合并输出。
func cmdCombined(cmd *exec.Cmd) ([]byte, error) {
	var out []byte
	err := inMainNetns(func() error {
		var e error
		out, e = cmd.CombinedOutput()
		return e
	})
	return out, err
}
