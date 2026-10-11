package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"mikugate/internal/runlock"
	"mikugate/internal/store"

	"mikugate/internal/management"
)

func runManagement(args []string, out io.Writer) error {
	if len(args) == 0 || (args[0] != "bootstrap-ip" && args[0] != "inspect" && args[0] != "refresh-ingress") {
		return errors.New("management bootstrap-ip --ip=数字IP --output=全新私有目录 --confirm | refresh-ingress --confirm --data=目录 --expected-instance=ID --expected-leaf=旧摘要 --origin=https://域名 --certificate=副本 --key=副本 | inspect --origin=https://地址 --certificate=文件 --key=文件 [--allow-bootstrap]")
	}
	f := flag.NewFlagSet("management", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	data := f.String("data", "", "已停止控制服务的私有数据目录")
	instance := f.String("expected-instance", "", "明确稳定实例")
	oldLeaf := f.String("expected-leaf", "", "明确旧管理证书叶摘要")
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
	if args[0] == "refresh-ingress" {
		if !*confirm || *data == "" || *instance == "" || *oldLeaf == "" || *origin == "" || *cert == "" || *key == "" || *ip != "" || *dir != "" || *bootstrap || runtime.GOOS == "linux" && os.Geteuid() == 0 {
			return errors.New("刷新入口证书需停止控制服务、专用普通账号、confirm/data/expected-instance/expected-leaf与明确域名TLS资产")
		}
		endpoint, err := management.ParseOrigin(*origin)
		if err != nil || endpoint.IP {
			return management.ErrHTTPS
		}
		_, asset, err := management.DomainAsset(endpoint, *cert, *key, endpoint.Hostname)
		if err != nil || asset == nil {
			return management.ErrHTTPS
		}
		if !filepath.IsAbs(*data) || filepath.Clean(*data) != *data {
			return errors.New("数据目录须明确绝对路径")
		}
		actual, err := store.InspectInstance(filepath.Join(*data, "miku-gate.db"))
		if err != nil || actual != *instance {
			return errors.New("入口证书更新实例不匹配，不创建新数据")
		}
		inspected, err := store.InspectMaintenance(filepath.Join(*data, "miku-gate.db"))
		if err != nil || (inspected.Schema != 6 && inspected.Schema != 7) {
			return errors.New("入口证书更新只支持已初始化schema6/7")
		}
		lease, err := runlock.Acquire(filepath.Join(*data, ".serve.lock"))
		if err != nil {
			return err
		}
		defer lease.Close()
		database, err := store.Open(filepath.Join(*data, "miku-gate.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		actual, err = database.InstanceID()
		if err != nil || actual != *instance {
			return errors.New("入口证书更新实例不匹配")
		}
		count, err := database.RefreshManagedCertificate(asset, *oldLeaf)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(map[string]any{"instance": actual, "updated_ingress_count": count, "leaf_sha256": asset.Summary().LeafSHA256, "identities_preserved": true, "private_material_returned": false})
	}
	if *data != "" || *instance != "" || *oldLeaf != "" {
		return errors.New("入口刷新专属参数不能混用")
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
