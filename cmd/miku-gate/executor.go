package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mikugate/internal/executoripc"
)

func runExecutor(args []string, output io.Writer) error {
	if len(args) == 0 || (args[0] != "serve" && args[0] != "info") {
		return errors.New("用法：miku-gate executor serve --instance=ID --allow-uid=UID --apply；普通用户可用 executor info --instance=ID 检查")
	}
	flags := flag.NewFlagSet("executor "+args[0], flag.ContinueOnError)
	flags.SetOutput(output)
	instance := flags.String("instance", "", "固定实例 ID，不允许请求切换实例")
	uid := flags.Uint64("allow-uid", 0, "只授权一个已有非 root 本机用户的 UID")
	apply := flags.Bool("apply", false, "明确启动仅本机 Unix socket 的隔离执行器")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不支持位置参数")
	}
	socket, err := executoripc.SocketPath(*instance)
	if err != nil {
		return err
	}
	if args[0] == "info" {
		if *apply || *uid != 0 {
			return errors.New("info 只读操作不接受 apply 或 allow-uid")
		}
		client, err := executoripc.NewClient(*instance)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		caps, err := client.Capabilities(ctx)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(output)
		enc.SetIndent("", "  ")
		return enc.Encode(caps)
	}
	if !*apply || *uid == 0 || *uid >= 1<<31 {
		return errors.New("未执行：serve 必须显式提供 --apply 和一个非 root --allow-uid")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return executoripc.Serve(ctx, *instance, uint32(*uid), func(string) {
		fmt.Fprintf(output, "隔离 IPC 已监听：%s · 仅 UID %d · 无 VPN / 代理数据面\n", socket, *uid)
	})
}
