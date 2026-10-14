//go:build linux

package executoripc

import (
	"context"
	"net"
	"os"
)

// VPNLease reuses the independently verified ownership/peer machinery, not the
// sandbox protocol. VPN messages live on a separate fixed-root Unix socket.
type VPNLease struct{ lease *socketLease }

func OpenVPNLease(instance string, uid uint32) (*VPNLease, error) {
	if os.Geteuid() != 0 {
		return nil, ErrPeer
	}
	p, err := makePolicy(instance, uid)
	if err != nil {
		return nil, err
	}
	lease, err := openLeaseAt(p, VPNRoot)
	if err != nil {
		return nil, err
	}
	return &VPNLease{lease}, nil
}
func (l *VPNLease) Listener() *net.UnixListener { return l.lease.listener }
func (l *VPNLease) Close() error                { return l.lease.close() }
func VPNPeerAllowed(conn *net.UnixConn, uid uint32) bool {
	p, err := peer(conn)
	return err == nil && p.Uid == uid
}
func DialVPN(ctx context.Context, instance string) (net.Conn, error) {
	return dialTrustedAt(ctx, instance, VPNRoot)
}
