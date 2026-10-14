package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// nativeClient 是一个可连接的客户端凭据。
// 复制入站时同一个 client 会挂到所有出口上，用户换出口只需要改端口。
type nativeClient struct {
	BoundTo  string `json:"bound_to,omitempty"`
	Email    string `json:"email"`
	ID       string `json:"id"`       // vless/vmess 用 UUID
	Password string `json:"password"` // trojan 用密码
	Enable   bool   `json:"enable"`
	// Flow 只对 VLESS 有意义，取值 "" 或 xtls-rprx-vision。
	// Vision 要求底层是 TCP + TLS/REALITY，其他组合下 Xray 会直接拒绝启动。
	Flow string `json:"flow,omitempty"`
}

// nativeInbound 是自建模式下的一个入站。
//
// 字段刻意贴着 3x-ui 的入站语义，这样两种后端在界面上表现一致。
type nativeInbound struct {
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // vless | vmess | trojan
	Network  string `json:"network"`  // tcp | ws | grpc | httpupgrade | xhttp
	Path     string `json:"path"`     // ws/httpupgrade/xhttp 路径，grpc 用作 serviceName
	Host     string `json:"host"`     // ws/httpupgrade/xhttp 的 Host 头
	// Security 是传输层安全：none | tls | reality
	Security string         `json:"security"`
	TLS      *tlsConfig     `json:"tls,omitempty"`
	Reality  *realityConfig `json:"reality,omitempty"`
	Remark   string         `json:"remark"`
	Enable   bool           `json:"enable"`
	Clients  []nativeClient `json:"clients"`
	// BoundTo 是绑定的节点主机名经 sanitizeTag 后的形式，空表示直连
	BoundTo       string `json:"bound_to"`
	AutoExitOrder uint64 `json:"auto_exit_order,omitempty"`
	AutoTemplate  int    `json:"auto_template,omitempty"`
}

// tlsConfig 是标准 TLS 的配置。证书要么由用户提供路径，要么 fanout 生成自签的。
type tlsConfig struct {
	ServerName string `json:"server_name"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	// SelfSigned 记录证书是 fanout 生成的，分享链接要带 allowInsecure
	SelfSigned bool `json:"self_signed"`
	// CertSha256 是证书的 SHA-256 指纹（十六进制）。
	// 自签证书客户端验不过，Xray 26.x 起 allowInsecure 已被移除，
	// 改为在链接里带指纹让客户端固定信任这一张证书。
	CertSha256 string `json:"cert_sha256,omitempty"`
}

// realityConfig 是 REALITY 的配置。
//
// PublicKey 服务端用不到，但客户端必须填，所以一并存下来供生成分享链接。
type realityConfig struct {
	Dest        string   `json:"dest"` // 借用的真实站点，如 www.microsoft.com:443
	ServerNames []string `json:"server_names"`
	PrivateKey  string   `json:"private_key"`
	PublicKey   string   `json:"public_key"`
	ShortIDs    []string `json:"short_ids"`
	Fingerprint string   `json:"fingerprint"` // 客户端指纹，如 chrome
}

// tag 复原这个入站在 Xray 里的 inboundTag，格式与 3x-ui 保持一致。
func (n *nativeInbound) tag() string {
	return fmt.Sprintf("in-%d-%s", n.Port, n.netOrTCP())
}

func (n *nativeInbound) netOrTCP() string {
	if n.Network == "" {
		return "tcp"
	}
	return n.Network
}

func (n *nativeInbound) securityOrNone() string {
	if n.Security == "" {
		return "none"
	}
	return n.Security
}

// nativeStore 是自建模式的持久状态。
type nativeStore struct {
	NextID   int              `json:"next_id"`
	Inbounds []*nativeInbound `json:"inbounds"`
}

func nativeStatePath(dir string) string { return filepath.Join(dir, "native.json") }

func loadNativeStore(dir string) (*nativeStore, error) {
	blob, err := os.ReadFile(nativeStatePath(dir))
	if os.IsNotExist(err) {
		return &nativeStore{NextID: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var st nativeStore
	if err := json.Unmarshal(blob, &st); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", nativeStatePath(dir), err)
	}
	if st.NextID < 1 {
		st.NextID = 1
	}
	return &st, nil
}

func (s *nativeStore) save(dir string) error {
	blob, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := nativeStatePath(dir) + ".tmp"
	if err := os.WriteFile(tmp, blob, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, nativeStatePath(dir))
}

func (s *nativeStore) byID(id int) *nativeInbound {
	for _, ib := range s.Inbounds {
		if ib.ID == id {
			return ib
		}
	}
	return nil
}

func (s *nativeStore) usedPorts() map[int]bool {
	// 先并入外部工具（如 xray-cf-lite）占用的端口，再叠加自己的入站，
	// 这样随机分配和手填校验都会避开它们，避免端口撞车。
	used := externalUsedPorts()
	for _, ib := range s.Inbounds {
		used[ib.Port] = true
	}
	return used
}

// sorted 返回按端口排序的入站，让界面顺序稳定。
func (s *nativeStore) sorted() []*nativeInbound {
	out := make([]*nativeInbound, len(s.Inbounds))
	copy(out, s.Inbounds)
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// newUUID 生成 Xray 认的 UUID v4。
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 随机源不可用时退回一个仍然唯一的形式，避免建站直接失败
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", os.Getpid())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return strings.Join([]string{h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]}, "-")
}

// randomHex 生成 n 字节的随机十六进制串，用作 trojan 密码与 ws 路径。
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprint(os.Getpid())))
	}
	return hex.EncodeToString(b)
}
