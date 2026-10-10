package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mikugate/internal/tunnel"
	"mikugate/internal/vpnipc"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func runVPNExecutor(args []string, output io.Writer) error {
	if len(args) == 0 || (args[0] != "serve" && args[0] != "info") {
		return errors.New("用法：vpn-executor serve --instance=ID --allow-uid=UID --tunnel-uid=UID --worker-uid=UID --apply；info只读查询")
	}
	flags := flag.NewFlagSet("vpn-executor "+args[0], flag.ContinueOnError)
	flags.SetOutput(output)
	instance := flags.String("instance", "", "本机固定实例ID")
	allowed := flags.Uint64("allow-uid", 0, "只授权一个已有普通用户UID")
	vpnUID := flags.Uint64("tunnel-uid", 0, "OpenVPN专属非root UID")
	workerUID := flags.Uint64("worker-uid", 0, "业务worker专属非root UID")
	apply := flags.Bool("apply", false, "明确启用独立的隔离VPN服务；不更改宿主网络")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return errors.New("VPN执行器参数无效")
	}
	client, err := vpnipc.New(*instance)
	if err != nil {
		return err
	}
	if args[0] == "info" {
		if *apply || *allowed != 0 || *vpnUID != 0 || *workerUID != 0 {
			return errors.New("info不接受权限/变更参数")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		caps, err := client.Capabilities(ctx)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(caps)
	}
	if !*apply || *allowed == 0 || *allowed >= 1<<31 || *vpnUID >= 1<<31 || *workerUID >= 1<<31 {
		return errors.New("未执行：需要显式apply、allow-uid和独立工作UID")
	}
	options := tunnel.SessionOptions{TunnelUID: uint32(*vpnUID), WorkerUID: uint32(*workerUID)}
	if options.Validate() != nil || *allowed == *vpnUID || *allowed == *workerUID {
		return errors.New("控制用户、OpenVPN和业务worker必须使用三个不同非root UID")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return vpnipc.Serve(ctx, *instance, uint32(*allowed), options, func(path string) {
		fmt.Fprintf(output, "隔离VPN服务已监听：%s · 仅UID%d · OpenVPN UID%d / worker UID%d · 不改宿主网络\n", path, *allowed, *vpnUID, *workerUID)
	})
}
