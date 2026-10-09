package egress

import "testing"

func TestDestinationPolicy(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.100.100.200", "::ffff:127.0.0.1", "::1", "2001:4860:4860::8888", "198.18.0.1", "192.0.2.1", "224.0.0.1", "0.0.0.0", "255.255.255.255", "host.example"} {
		if PublicIPv4(ip) == nil {
			t.Errorf("accepted %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "::ffff:1.1.1.1"} {
		if err := PublicIPv4(ip); err != nil {
			t.Errorf("rejected %s: %v", ip, err)
		}
	}
}
