package egress

import (
	"errors"
	"net/netip"
)

var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

// PublicIPv4 rejects IPv6 and non-public destinations, including IPv4-mapped aliases.
func PublicIPv4(raw string) error {
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return errors.New("目标不是有效 IP")
	}
	ip = ip.Unmap()
	if !ip.Is4() {
		return errors.New("当前版本不允许业务 IPv6")
	}
	for _, p := range blocked {
		if p.Contains(ip) {
			return errors.New("默认禁止访问私网、保留地址或元数据网络")
		}
	}
	return nil
}
