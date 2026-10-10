package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"syscall"
	"time"

	"mikugate/internal/diagnostics"
	"mikugate/internal/domain"
	"mikugate/internal/executoripc"
	"mikugate/internal/gateway"
	"mikugate/internal/hostinventory"
	"mikugate/internal/httpapi"
	"mikugate/internal/runlock"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"mikugate/internal/tunnel"
	"mikugate/internal/vpnipc"
	"mikugate/web"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) == 2 && os.Args[1] == "_vpn-openvpn" {
		return tunnel.RunOpenVPN()
	}
	if len(os.Args) == 2 && os.Args[1] == "_vpn-supervisor" {
		return tunnel.RunSupervisor()
	}
	if len(os.Args) == 2 && os.Args[1] == "_vpn-worker" {
		return tunnel.RunWorker()
	}
	if len(os.Args) < 2 {
		fmt.Println("Miku Gate " + domain.Version + "\n用法：miku-gate init | instance | serve | doctor | inventory | ingress | version | sandbox | executor | vpn-executor\n支持本机认证代理上游；Linux 隔离 OpenVPN 已接线；交互安装与加密入口开发中。")
		return nil
	}
	if os.Args[1] == "version" {
		fmt.Println(domain.Version)
		return nil
	}
	if os.Args[1] == "vpn-executor" {
		return runVPNExecutor(os.Args[2:], os.Stdout)
	}
	if os.Args[1] == "executor" {
		return runExecutor(os.Args[2:], os.Stdout)
	}
	if os.Args[1] == "sandbox" {
		return runSandbox(os.Args[2:], os.Stdout)
	}
	if os.Args[1] == "inventory" {
		if len(os.Args) != 2 {
			return errors.New("inventory 不接受路径或命令参数")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(hostinventory.Inspect(ctx))
	}
	if os.Args[1] == "ingress" {
		return runIngress(os.Args[2:], os.Stdin, os.Stdout)
	}
	if os.Args[1] == "doctor" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(diagnostics.Inspect())
	}
	if os.Args[1] != "init" && os.Args[1] != "serve" && os.Args[1] != "instance" {
		return errors.New("未知命令")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	data := flags.String("data", ".data", "数据目录（包含凭据摘要和配置）")
	username := flags.String("username", "admin", "初始化管理员名称")
	sandboxIPC := flags.Bool("sandbox-ipc", false, "显式接入本实例 Unix socket 隔离执行器；不启用 VPN")
	vpnExecutor := flags.Bool("vpn-executor", false, "显式接入独立最小权限OpenVPN执行器")
	controlOnly := flags.Bool("control-plane-only", false, "禁用代理上游运行时，仅保留配置控制台")
	maxActive := flags.Int("max-active", 3, "同时运行的代理出口上限，1–8（不是容量保证）")
	listen := flags.String("listen", "127.0.0.1:3939", "本机管理监听地址")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("存在不支持的位置参数")
	}
	if *vpnExecutor && os.Args[1] != "serve" {
		return errors.New("--vpn-executor只适用于serve")
	}
	if *sandboxIPC && os.Args[1] != "serve" {
		return errors.New("--sandbox-ipc 只适用于 serve")
	}
	if os.Args[1] == "instance" {
		id, err := store.InspectInstance(filepath.Join(*data, "miku-gate.db"))
		if err != nil {
			return err
		}
		fmt.Println(id)
		return nil
	}
	if os.Args[1] == "serve" && runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return errors.New("控制服务不应作为 root 运行；请使用独立普通用户和受保护的数据目录")
	}
	if *maxActive < 1 || *maxActive > 8 {
		return errors.New("--max-active 必须在1–8之间")
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		return err
	}
	lease, err := runlock.Acquire(filepath.Join(*data, ".serve.lock"))
	if err != nil {
		return err
	}
	defer lease.Close()
	s, err := store.Open(filepath.Join(*data, "miku-gate.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	if os.Args[1] == "init" {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`).MatchString(*username) {
			return errors.New("用户名需为 1–40 位字母、数字、下划线或短横线")
		}
		pass, err := security.Token(18)
		if err != nil {
			return err
		}
		hash, err := security.HashPassword(pass)
		if err != nil {
			return err
		}
		if err = s.InitAccount(*username, hash); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				return errors.New("管理员已存在，未覆盖现有密码")
			}
			return err
		}
		fmt.Printf("管理员已创建。密码只显示一次，请妥善保存。\n用户名：%s\n密码：%s\n", *username, pass)
		return nil
	}
	initialized, err := s.Initialized()
	if err != nil {
		return err
	}
	if !initialized {
		return errors.New("请先运行 miku-gate init 初始化本机管理员")
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("当前开发版本仅允许绑定回环 IP，不允许公网监听")
	}
	assets, err := web.Assets()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var options httpapi.Options
	if *vpnExecutor {
		if *controlOnly {
			return errors.New("--vpn-executor不能用于纯控制台模式")
		}
		instance, err := s.InstanceID()
		if err != nil {
			return err
		}
		client, err := vpnipc.New(instance)
		if err != nil {
			return err
		}
		options.VPN = client
	}
	if !*controlOnly {
		manager, err := gateway.New(ctx, s, gateway.Options{MaxExits: *maxActive, Factory: gateway.WithVPN(s, options.VPN)})
		if err != nil {
			return err
		}
		options.Gateway = manager
		defer func() {
			closing, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := manager.Close(closing); err != nil {
				log.Printf("代理运行时关闭未全部确认: %v", err)
			}
		}()
	}

	if *sandboxIPC {
		instance, err := s.InstanceID()
		if err != nil {
			return err
		}
		client, err := executoripc.NewClient(instance)
		if err != nil {
			return err
		}
		options.Sandbox = client
	}
	handler, err := httpapi.NewWithOptions(s, assets, options)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: handler, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
		}
	}()
	log.Printf("Miku Gate %s · http://%s · 代理上游运行时=%t · 隔离 OpenVPN 配置=%t · 沙箱 IPC 配置=%t", domain.Version, listener.Addr(), !*controlOnly, *vpnExecutor, *sandboxIPC)
	err = server.Serve(listener)
	stop()
	<-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
