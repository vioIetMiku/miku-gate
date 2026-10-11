package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"path/filepath"

	"mikugate/internal/certassets"
	"mikugate/internal/ingress"
	"mikugate/internal/wirejson"
)

func runManagedIngress(args []string, input io.Reader, out io.Writer) error {
	if len(args) == 1 && args[0] == "templates" {
		return json.NewEncoder(out).Encode(ingress.Templates())
	}
	if len(args) == 1 && args[0] == "reality-candidates" {
		return json.NewEncoder(out).Encode(ingress.RealityCandidates())
	}
	f := flag.NewFlagSet("ingress "+args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	host := f.String("domain", "", "TLS域名/REALITY目标域名")
	read := f.Bool("confirm-read", false, "确认读取域名证书材料，私钥不回显")
	network := f.Bool("confirm-network", false, "确认VPS公开TCP443的TLS1.3目标探测")
	create := f.Bool("confirm-create", false, "确认生成全新私有bundle，不启动/覆盖现有服务")
	dir := f.String("bundle-dir", "", "新建私有bundle的规范绝对目录")
	if e := f.Parse(args[1:]); e != nil || f.NArg() != 0 {
		return errors.New("受管入口参数无效")
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	switch args[0] {
	case "certificate":
		if !*read || *host == "" || *create || *dir != "" || *network {
			return errors.New("certificate需--domain和--confirm-read，不接受创建或网络参数")
		}
		report, _, e := certassets.Discover(*host)
		if e != nil {
			return e
		}
		return enc.Encode(report)
	case "reality-check":
		if !*network || *host == "" || *create || *dir != "" || *read {
			return errors.New("reality-check需--domain和--confirm-network，仅单个目标固定443TLS探测")
		}
		target, e := ingress.ProbeReality(context.Background(), *host)
		if e != nil {
			return e
		}
		return enc.Encode(target)
	case "managed":
		if !*create || *dir == "" || *host != "" {
			return errors.New("managed需--bundle-dir和--confirm-create；request/proxy材料仅从stdin读取，不在参数里放密码")
		}
		raw, e := io.ReadAll(io.LimitReader(input, 8193))
		if e != nil || len(raw) > 8192 {
			return errors.New("受管入口stdin超出限制")
		}
		var body struct {
			Request ingress.ManagedRequest `json:"request"`
			Proxy   ingress.Proxy          `json:"proxy"`
		}
		if e = decodeManagedInput(raw, &body); e != nil {
			return e
		}
		plan, e := ingress.ManagedPlanFor(body.Request)
		if e != nil {
			return e
		}
		var asset *certassets.Asset
		var target *ingress.RealityTarget
		if plan.NeedsCertificate {
			if !*read {
				return errors.New("TLS模板需要--confirm-read自动查找证书，无需手填私钥")
			}
			_, asset, e = certassets.Discover(plan.Request.ServerName)
			if e != nil {
				return e
			}
			if asset == nil {
				return errors.New("未发现已验证域名证书，未生成入口；后续安装向导可申请或导入受控证书")
			}
		}
		if plan.NeedsRealityTarget {
			if !*network {
				return errors.New("REALITY模板需要--confirm-network先检查目标")
			}
			checked, e := ingress.ProbeReality(context.Background(), plan.Request.RealityHostname)
			if e != nil {
				return e
			}
			target = &checked
		}
		files := ingress.ManagedFiles{Certificate: filepath.Join(*dir, "certificate.pem"), PrivateKey: filepath.Join(*dir, "private.key")}
		bundle, e := ingress.GenerateManaged(body.Request, body.Proxy, asset, files, target)
		if e != nil {
			return e
		}
		contents := map[string][]byte{"server.json": bundle.ServerJSON, "client.json": bundle.ClientJSON, "client-uri.txt": []byte(bundle.URI + "\n")}
		if asset != nil {
			contents["certificate.pem"], contents["private.key"] = asset.PEM()
		}
		meta, e := json.MarshalIndent(bundle, "", "  ")
		if e != nil {
			return errors.New("bundle元数据生成失败")
		}
		contents["BUNDLE.json"] = append(meta, '\n')
		if e = writeManagedDirectory(*dir, contents); e != nil {
			return e
		}
		return enc.Encode(bundle)
	}
	return errors.New("未知受管入口命令")
}

func decodeManagedInput(raw []byte, dst any) error {
	if wirejson.Decode(raw, dst, 8192) != nil {
		return errors.New("stdin需严格JSON request/proxy，不回显无效材料")
	}
	return nil
}
