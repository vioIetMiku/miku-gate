package gateway

import (
	"context"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/vpnipc"
	"net"
)

var ErrVPNDisabled = errors.New("OpenVPN配置已保存，但控制服务未显式接入独立VPN执行器；需要--vpn-executor")

type VPNSource interface {
	VPN(string) (domain.VPNConfig, bool, error)
}

func WithVPN(source VPNSource, client *vpnipc.Client) Factory {
	return func(ctx context.Context, exit domain.Exit, upstream *domain.UpstreamConfig) (Session, error) {
		if upstream != nil {
			return proxyFactory(ctx, exit, upstream)
		}
		config, exists, err := source.VPN(exit.ID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrUnavailable
		}
		if blocks, ok := source.(interface{ NodeBlocked(string) (bool, error) }); ok {
			blocked, err := blocks.NodeBlocked(config.NodeID)
			if err != nil || blocked {
				return nil, errors.New("所选VPN节点已屏蔽")
			}
		}
		if client == nil {
			return nil, ErrVPNDisabled
		}
		var front *vpnipc.TransportProxy
		if config.FrontProxy != nil {
			front = &vpnipc.TransportProxy{Config: *config.FrontProxy, Password: config.FrontProxy.Password}
		}
		session, err := client.Start(ctx, exit.ID, vpnipc.Config{Front: front, Profile: config.Profile, IP: config.IP, PeerName: config.PeerName, PeerFingerprint: config.PeerFingerprint, AllowLegacy: config.AllowLegacy})
		if err != nil {
			return nil, err
		}
		transport := "direct"
		if config.FrontProxy != nil {
			transport = config.FrontProxy.Kind
		}
		return &vpnSession{Session: session, transport: transport}, nil
	}
}

type vpnSession struct {
	*vpnipc.Session
	transport string
}

func (s *vpnSession) Probe(ctx context.Context) (Proof, error) {
	ip, err := s.Session.Probe(ctx)
	if err != nil {
		return Proof{}, err
	}
	return Proof{Kind: "openvpn", ProtectionMode: "namespace", VPNTransport: s.transport, DNSVerified: true, KernelVerified: true, ExitIP: ip}, nil
}

func (s *vpnSession) DialUDP(ctx context.Context, address string) (net.Conn, error) {
	return s.Session.DialUDP(ctx, address)
}
