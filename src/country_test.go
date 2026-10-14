package main

import "testing"

func TestFlagOf(t *testing.T) {
	cases := map[string]string{
		"JP":   "🇯🇵",
		"jp":   "🇯🇵",
		"US":   "🇺🇸",
		"HK":   "🇭🇰",
		" kr ": "🇰🇷",
		"":     "",
		"J":    "",
		"JPN":  "",
		"J1":   "",
	}
	for in, want := range cases {
		if got := flagOf(in); got != want {
			t.Fatalf("flagOf(%q)=%q，想要 %q", in, got, want)
		}
	}
}

func TestCountryLabel(t *testing.T) {
	cases := []struct {
		code, fallback, want string
	}{
		{"JP", "Japan", "🇯🇵 日本"},
		{"jp", "Japan", "🇯🇵 日本"},
		{"KR", "Korea Republic of", "🇰🇷 韩国"},
		{"TW", "Taiwan", "🇹🇼 台湾"},
		// 中文表没收录时回退到清单给的英文名，但旗还在
		{"XK", "Kosovo", "🇽🇰 Kosovo"},
		// 连英文名也没有就只剩国家码
		{"XK", "", "🇽🇰 XK"},
		// 国家码不合法时只留名字，不能吐出半个旗
		{"", "Japan", "Japan"},
		{"JPN", "Japan", "Japan"},
	}
	for _, c := range cases {
		if got := countryLabel(c.code, c.fallback); got != c.want {
			t.Fatalf("countryLabel(%q,%q)=%q，想要 %q", c.code, c.fallback, got, c.want)
		}
	}
}

// 别名要能一眼看出国家，末段留着区分同国的多条出口。
func TestExitLabel(t *testing.T) {
	t1 := &Tunnel{Node: Node{CountryCode: "JP", Country: "Japan"}, ExitIP: "133.32.233.192"}
	if got := exitLabel(t1); got != "🇯🇵 日本 192" {
		t.Fatalf("exitLabel=%q", got)
	}
	// 还没探到出口 IP 时退回主机名
	t2 := &Tunnel{Node: Node{CountryCode: "KR", Country: "Korea", HostName: "vpn123"}}
	if got := exitLabel(t2); got != "🇰🇷 韩国 vpn123" {
		t.Fatalf("exitLabel=%q", got)
	}
	// 同国两条出口的别名不能撞（mihomo 要求节点名唯一）
	a := &Tunnel{Node: Node{CountryCode: "JP", Country: "Japan"}, ExitIP: "1.2.3.4"}
	b := &Tunnel{Node: Node{CountryCode: "JP", Country: "Japan"}, ExitIP: "1.2.3.5"}
	if exitLabel(a) == exitLabel(b) {
		t.Fatal("同国不同出口的别名撞了")
	}
	// 什么国家信息都没有时不能只剩一个空名字
	t3 := &Tunnel{Node: Node{HostName: "vpn999"}}
	if got := exitLabel(t3); got != "vpn999" {
		t.Fatalf("exitLabel=%q", got)
	}
}

// 中文表里不该有空值或没配对的国家码。
func TestCountryTableSane(t *testing.T) {
	for code, name := range countryCN {
		if len(code) != 2 {
			t.Fatalf("国家码 %q 不是两位", code)
		}
		if flagOf(code) == "" {
			t.Fatalf("国家码 %q 出不了国旗", code)
		}
		if name == "" {
			t.Fatalf("%s 没有中文名", code)
		}
	}
}
