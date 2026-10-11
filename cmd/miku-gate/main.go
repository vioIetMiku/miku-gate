package main

import (
	"context"
	"crypto/tls"
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
	"strconv"
	"syscall"
	"time"

	"mikugate/internal/certassets"
	"mikugate/internal/coreruntime"
	"mikugate/internal/diagnostics"
	"mikugate/internal/domain"
	"mikugate/internal/executoripc"
	"mikugate/internal/gateway"
	"mikugate/internal/hostinventory"
	"mikugate/internal/httpapi"
	"mikugate/internal/management"
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
		fmt.Println("Miku Gate " + domain.Version + "\n用法：miku-gate init | instance | data-check | serve | doctor | inventory | ingress | management | version | sandbox | executor | vpn-executor\n支持本机认证代理上游；Linux 隔离 OpenVPN 已接线；交互安装与加密入口开发中。")
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
	if os.Args[1] == "management" {
		return runManagement(os.Args[2:], os.Stdout)
	}
	if os.Args[1] == "doctor" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(diagnostics.Inspect())
	}
	if os.Args[1] != "init" && os.Args[1] != "serve" && os.Args[1] != "instance" && os.Args[1] != "data-check" {
		return errors.New("未知命令")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	data := flags.String("data", ".data", "数据目录（包含凭据摘要和配置）")
	username := flags.String("username", "admin", "初始化管理员名称")
	sandboxIPC := flags.Bool("sandbox-ipc", false, "显式接入本实例 Unix socket 隔离执行器；不启用 VPN")
	vpnExecutor := flags.Bool("vpn-executor", false, "显式接入独立最小权限OpenVPN执行器")
	controlOnly := flags.Bool("control-plane-only", false, "禁用代理上游运行时，仅保留配置控制台")
	xrayPath := flags.String("xray-core", "", "显式信任的root所有Xray可执行文件绝对路径；不自动执行已有面板程序")
	singPath := flags.String("sing-box-core", "", "显式信任的root所有sing-box可执行文件绝对路径")
	maxActive := flags.Int("max-active", 3, "同时运行的代理出口上限，1–8（不是容量保证）")
	publicOrigin := flags.String("public-origin", "", "明确HTTPS管理Origin，仅该Host可访问，默认保持回环HTTP")
	tlsCertificate := flags.String("tls-certificate", "", "只读管理TLS证书绝对路径")
	tlsKey := flags.String("tls-key", "", "受保护管理私钥绝对路径，不回显")
	allowBootstrap := flags.Bool("allow-bootstrap-certificate", false, "仅IP模式明确允许自签引导证书；需要客户端核对指纹/导入信任")
	listen := flags.String("listen", "127.0.0.1:3939", "本机管理监听地址")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if (*xrayPath != "" || *singPath != "") && (os.Args[1] != "serve" || *controlOnly) {
		return errors.New("受管核心路径仅用于具备出口运行时的serve")
	}
	if flags.NArg() != 0 {
		return errors.New("存在不支持的位置参数")
	}
	if (*publicOrigin != "" || *tlsCertificate != "" || *tlsKey != "" || *allowBootstrap) && os.Args[1] != "serve" {
		return errors.New("HTTPS管理参数只用于serve")
	}
	if *vpnExecutor && os.Args[1] != "serve" {
		return errors.New("--vpn-executor只适用于serve")
	}
	if *sandboxIPC && os.Args[1] != "serve" {
		return errors.New("--sandbox-ipc 只适用于 serve")
	}
	if os.Args[1] == "data-check" {
		if runtime.GOOS == "linux" && os.Geteuid() == 0 {
			return errors.New("数据维护检查须使用专用普通账号，避免root生成数据库侧文件")
		}
		checked, err := store.InspectMaintenance(filepath.Join(*data, "miku-gate.db"))
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(checked)
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
	host, listenPort, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("管理监听必须数字IP")
	}
	var managementTLS *tls.Config
	var managementIdentity management.CertificateInfo
	if *publicOrigin == "" {
		if !ip.IsLoopback() || *tlsCertificate != "" || *tlsKey != "" || *allowBootstrap {
			return errors.New("明文管理仅允许回环监听；公网须显式public-origin和TLS素材")
		}
	} else {
		endpoint, e := management.ParseOrigin(*publicOrigin)
		if e != nil {
			return e
		}
		if listenPort != strconv.Itoa(endpoint.Port) {
			return errors.New("管理监听端口与公开HTTPS地址不匹配")
		}
		managementTLS, managementIdentity, err = management.Load(endpoint, *tlsCertificate, *tlsKey, *allowBootstrap)
		if err != nil {
			return err
		}
	}

	assets, err := web.Assets()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var options httpapi.Options
	options.PublicOrigin = *publicOrigin
	if managementTLS != nil {
		options.ManagementIdentity = &managementIdentity
		endpoint, err := management.ParseOrigin(*publicOrigin)
		if err != nil {
			return err
		}
		if !endpoint.IP && managementIdentity.SystemTrusted {
			options.DiscoverCertificate = func(host string) (certassets.Discovery, *certassets.Asset, error) {
				normalized, err := certassets.Hostname(host)
				if err != nil {
					return certassets.Discovery{}, nil, err
				}
				if normalized == endpoint.Hostname {
					return management.DomainAsset(endpoint, *tlsCertificate, *tlsKey, normalized)
				}
				return certassets.Discover(normalized)
			}
		}
	}
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

	if *xrayPath != "" || *singPath != "" {
		cores, err := coreruntime.New(ctx, s, options.Gateway, coreruntime.Options{XrayPath: *xrayPath, SingBoxPath: *singPath, MaxActive: *maxActive})
		if err != nil {
			return err
		}
		options.Cores = cores
		defer func() {
			closing, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if cores.Close(closing) != nil {
				log.Print("受管核心停止未确认")
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
	server := &http.Server{TLSConfig: managementTLS, Addr: *listen, Handler: handler, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
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
	scheme := "http"
	if managementTLS != nil {
		scheme = "https"
	}
	log.Printf("Miku Gate %s · %s://%s · 代理上游运行时=%t · 隔离 OpenVPN 配置=%t · 沙箱 IPC 配置=%t", domain.Version, scheme, listener.Addr(), !*controlOnly, *vpnExecutor, *sandboxIPC)
	if managementTLS != nil {
		log.Printf("管理HTTPS %s · 证书SHA256=%s · 系统信任=%t · IP自签引导=%t（引导身份需用户核对/导入，不是公共CA信任）", managementIdentity.Origin, managementIdentity.LeafSHA256, managementIdentity.SystemTrusted, managementIdentity.Bootstrap)
		err = server.ServeTLS(listener, "", "")
	} else {
		err = server.Serve(listener)
	}
	stop()
	<-shutdownDone
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
