package main

import "fmt"

// Old slots retain their 10.base.slot.0/30 addresses. New slots use unused /30s
// in the same owned /16 instead of overflowing an IPv4 octet after slot 255.
const linkSubnetCapacity = 256*64 - 1

func (t *Tunnel) linkAddresses() (host, peer, cidr string, err error) {
	if t.Slot < 0 || t.Slot > linkSubnetCapacity {
		return "", "", "", fmt.Errorf("网络槽位地址无效")
	}
	third, fourth := t.Slot, 0
	if t.Slot >= 256 {
		extra := t.Slot - 256
		third, fourth = extra/63, 4+(extra%63)*4
	}
	prefix := fmt.Sprintf("10.%d.%d.", instBase, third)
	return fmt.Sprintf("%s%d", prefix, fourth+1), fmt.Sprintf("%s%d", prefix, fourth+2), fmt.Sprintf("%s%d/30", prefix, fourth), nil
}
