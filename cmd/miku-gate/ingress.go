package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"mikugate/internal/ingress"
	"mikugate/internal/wirejson"
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
	raw, e := ingress.ReadSource(source)
	if e != nil {
		return e
	}
	if args[0] == "inspect" {
		report, e := ingress.Inspect(source, raw)
		if e != nil {
			return e
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
