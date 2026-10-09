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

	"mikugate/internal/netexec"
)

func runSandbox(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("用法：miku-gate sandbox plan|inspect|prepare|cleanup --instance=ID --exit=ID；变更操作还需 --apply")
	}
	operation := args[0]
	if operation != "plan" && operation != "inspect" && operation != "prepare" && operation != "cleanup" {
		return errors.New("未知 sandbox 操作")
	}
	flags := flag.NewFlagSet("sandbox "+operation, flag.ContinueOnError)
	flags.SetOutput(output)
	instanceID := flags.String("instance", "", "本实例的持久化 ID（见网络计划预览）")
	exitID := flags.String("exit", "", "出口的稳定 ID")
	apply := flags.Bool("apply", false, "明确执行隔离沙箱变更；不建立 VPN 或代理")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("存在不支持的位置参数")
	}
	plan, err := netexec.BuildPlan(*instanceID, *exitID)
	if err != nil {
		return err
	}
	mutating := operation == "prepare" || operation == "cleanup"
	if mutating && !*apply {
		return errors.New("未执行变更：prepare/cleanup 必须显式传入 --apply；可先使用 sandbox plan 预览")
	}
	if !mutating && *apply {
		return errors.New("只读 plan/inspect 不接受 --apply")
	}
	enc := json.NewEncoder(output)
	enc.SetIndent("", "  ")
	if operation == "plan" {
		return enc.Encode(plan)
	}
	engine, err := netexec.Open(*instanceID, mutating)
	if err != nil {
		return err
	}
	defer engine.Close()
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signals, 30*time.Second)
	defer cancel()
	var status netexec.Result
	switch operation {
	case "inspect":
		status, err = engine.Inspect(ctx, *exitID)
	case "prepare":
		status, err = engine.Prepare(ctx, *exitID)
	case "cleanup":
		status, err = engine.Cleanup(ctx, *exitID)
	}
	if encodingErr := enc.Encode(status); encodingErr != nil {
		return errors.Join(err, encodingErr)
	}
	if err != nil {
		return fmt.Errorf("sandbox %s: %w", operation, err)
	}
	return nil
}
