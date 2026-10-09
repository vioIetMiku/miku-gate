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
	"mikugate/internal/httpapi"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"mikugate/web"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		fmt.Println("Miku Gate " + domain.Version + "\n用法：miku-gate init | instance | serve | doctor | version | sandbox | executor\n当前为控制台开发版本，不建立 VPN 隧道。")
		return nil
	}
	if os.Args[1] == "version" {
		fmt.Println(domain.Version)
		return nil
	}
	if os.Args[1] == "executor" {
		return runExecutor(os.Args[2:], os.Stdout)
	}
	if os.Args[1] == "sandbox" {
		return runSandbox(os.Args[2:], os.Stdout)
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
	listen := flags.String("listen", "127.0.0.1:3939", "本机管理监听地址")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("存在不支持的位置参数")
	}
	if *sandboxIPC && os.Args[1] != "serve" {
		return errors.New("--sandbox-ipc 只适用于 serve")
	}
	if os.Args[1] == "instance" {
		if _, err := os.Stat(filepath.Join(*data, "miku-gate.db")); err != nil {
			return errors.New("实例数据库不存在；请先由普通用户执行 init")
		}
	}
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
	if os.Args[1] == "instance" {
		id, err := s.InstanceID()
		if err != nil {
			return err
		}
		fmt.Println(id)
		return nil
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("当前开发版本仅允许绑定回环 IP，不允许公网监听")
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return errors.New("控制服务不应作为 root 运行；请使用独立普通用户和受保护的数据目录")
	}
	assets, err := web.Assets()
	if err != nil {
		return err
	}
	var options httpapi.Options
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
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("Miku Gate %s · http://%s · VPN 数据面未启用 · 隔离 IPC 配置=%t", domain.Version, listener.Addr(), *sandboxIPC)
	if err = server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
