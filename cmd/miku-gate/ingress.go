package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"mikugate/internal/ingress"
	"mikugate/internal/wirejson"
	"strconv"
)

func runIngress(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 1 && args[0] == "sources" {
		return json.NewEncoder(output).Encode(ingress.Sources())
	}
	if len(args) < 1 || (args[0] != "inspect" && args[0] != "prepare") {
		return errors.New("用法：ingress sources | inspect --source=固定来源 --confirm-read | prepare --source=固定来源 --confirm-read --inbound=检测ID --expected-sha256=摘要 --output=新文件 --confirm-create（认证材料通过stdin JSON）")
	}
	f := flag.NewFlagSet("ingress "+args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	sourceID := f.String("source", "", "固定来源ID")
	owner := f.String("allow-install-owner", "", "仅inspect：明确确认x-ui两个安装目录的数字UID，不授权执行/应用")
	confirm := f.Bool("confirm-read", false, "明确同意读取现有核心配置")
	selected := f.String("inbound", "", "检测到的入口ID")
	expected := f.String("expected-sha256", "", "检测时源摘要")
	destination := f.String("output", "", "仅新建候选文件")
	create := f.Bool("confirm-create", false, "确认生成含认证材料的私有候选")
	if e := f.Parse(args[1:]); e != nil || f.NArg() != 0 {
		return errors.New("入口命令参数无效")
	}
	if !*confirm {
		return errors.New("读取核心配置需要 --confirm-read；不会读取面板数据库或证书私钥文件")
	}
	if args[0] == "inspect" && (*selected != "" || *expected != "" || *destination != "" || *create) {
		return errors.New("inspect 不接受创建候选参数")
	}
	source, e := ingress.FindSource(*sourceID)
	if e != nil {
		return e
	}
	var raw []byte
	if *owner != "" {
		if args[0] != "inspect" {
			return errors.New("--allow-install-owner只授权只读摘要，不允许prepare或应用")
		}
		uid, err := strconv.ParseUint(*owner, 10, 32)
		if err != nil || uid == 0 {
			return errors.New("安装目录所有者必须是明确非root数字UID")
		}
		raw, e = ingress.ReadPanelSource(source, uint32(uid))
	} else {
		raw, e = ingress.ReadSource(source)
	}
	if e != nil {
		return e
	}
	if args[0] == "inspect" {
		report, e := ingress.Inspect(source, raw)
		if e != nil {
			return e
		}
		ingress.ObserveListeners(&report)
		if *owner != "" {
			report.Warnings = append(report.Warnings, "管理员明确确认安装目录UID="+*owner+"作只读摘要；不信任该目录的二进制、不授权候选生成/应用，仍需要隔离迁移或后续受控接入。")
		}
		enc := json.NewEncoder(output)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if !*create || *destination == "" {
		return errors.New("prepare 需 --confirm-create 和新的私有 --output 路径；不会替换运行配置")
	}
	creds, e := io.ReadAll(io.LimitReader(input, 8193))
	if e != nil || len(creds) > 8192 {
		return errors.New("stdin认证材料超出上限")
	}
	var proxy ingress.Proxy
	if e = wirejson.Decode(bytes.TrimSpace(creds), &proxy, 8192); e != nil {
		return errors.New("stdin需严格JSON：port、username、password；不在参数或日志传密码")
	}
	candidate, plan, e := ingress.Prepare(source, raw, *expected, *selected, proxy)
	if e != nil {
		return e
	}
	if e = writePrivateCandidate(*destination, candidate); e != nil {
		return e
	}
	enc := json.NewEncoder(output)
	enc.SetIndent("", "  ")
	return enc.Encode(plan)
}
