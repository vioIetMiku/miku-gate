package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"

	"mikugate/internal/management"
)

func runManagement(args []string, out io.Writer) error {
	if len(args) == 0 || (args[0] != "bootstrap-ip" && args[0] != "inspect") {
		return errors.New("management bootstrap-ip --ip=数字IP --output=全新私有目录 --confirm | inspect --origin=https://地址 --certificate=文件 --key=文件 [--allow-bootstrap]")
	}
	f := flag.NewFlagSet("management", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	ip := f.String("ip", "", "数字IP")
	dir := f.String("output", "", "全新目录")
	origin := f.String("origin", "", "明确HTTPS地址")
	cert := f.String("certificate", "", "管理证书")
	key := f.String("key", "", "管理私钥")
	confirm := f.Bool("confirm", false, "确认不受公共CA信任的IP引导证书")
	bootstrap := f.Bool("allow-bootstrap", false, "明确只为IP接受自签引导身份，需核对指纹")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 {
		return errors.New("management参数无效")
	}
	var info management.CertificateInfo
	var e error
	if args[0] == "bootstrap-ip" {
		if !*confirm || *ip == "" || *dir == "" || *origin != "" || *cert != "" || *key != "" || *bootstrap {
			return errors.New("生成IP引导身份需要明确--confirm、--ip和--output，不覆盖原证书")
		}
		info, e = management.BootstrapIP(*ip, *dir)
	} else {
		if *confirm || *ip != "" || *dir != "" || *origin == "" || *cert == "" || *key == "" {
			return errors.New("inspect需origin/certificate/key，仅只读验证")
		}
		info, e = management.ValidateFiles(*origin, *cert, *key, *bootstrap)
	}
	if e != nil {
		return e
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}
