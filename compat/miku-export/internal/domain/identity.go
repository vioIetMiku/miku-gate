package domain

import "time"

// NodeIdentity records a point-in-time TLS check, not VPN connectivity or node
// operator reputation. Its certificate fingerprint binds the later VPN session.
type NodeIdentity struct {
	NodeID          string    `json:"node_id"`
	IP              string    `json:"ip"`
	TLSHost         string    `json:"tls_host"`
	TLSPort         uint16    `json:"tls_port"`
	PeerName        string    `json:"peer_name"`
	PeerFingerprint string    `json:"peer_fingerprint"`
	ProfileSHA256   string    `json:"profile_sha256"`
	TLSVersion      string    `json:"tls_version"`
	CheckedAt       time.Time `json:"checked_at"`
	NotAfter        time.Time `json:"not_after"`
	Source          string    `json:"source"`
	Verified        bool      `json:"verified"`
	VPNConnected    bool      `json:"vpn_connected"`
	Message         string    `json:"message"`
}
