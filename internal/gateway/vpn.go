package gateway

import (
	"context"
	"errors"
	"mikugate/internal/domain"
	"mikugate/internal/vpnipc"
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
		if client == nil {
			return nil, ErrVPNDisabled
		}
		session, err := client.Start(ctx, exit.ID, vpnipc.Config{Profile: config.Profile, IP: config.IP, PeerName: config.PeerName, PeerFingerprint: config.PeerFingerprint, AllowLegacy: config.AllowLegacy})
		if err != nil {
			return nil, err
		}
		return &vpnSession{Session: session}, nil
	}
}

type vpnSession struct{ *vpnipc.Session }

func (s *vpnSession) Probe(ctx context.Context) (Proof, error) {
	ip, err := s.Session.Probe(ctx)
	if err != nil {
		return Proof{}, err
	}
	return Proof{Kind: "openvpn", ProtectionMode: "namespace", DNSVerified: true, KernelVerified: true, ExitIP: ip}, nil
}
