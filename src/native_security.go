package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// checkRealityDest 确认 dest 能完成 TLS1.3 握手。
//
// REALITY 会把每个连接都转给 dest 做一次真实握手，dest 握手走不完时
// 服务端只会静默回落，客户端看到的是 EOF，很难查。宁可建站时就报错。
func checkRealityDest(dest, serverName string) error {
	conn, err := net.DialTimeout("tcp", dest, 8*time.Second)
	if err != nil {
		return fmt.Errorf("连不上 %s: %w", dest, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	c := tls.Client(conn, &tls.Config{
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	})
	if err := c.Handshake(); err != nil {
		return fmt.Errorf("%s 的 TLS1.3 握手失败: %w", dest, err)
	}
	return nil
}

// realityKeys 调用 xray 生成一对 X25519 密钥。
//
// 不同版本的输出措辞不一样：新版是 "Password (PublicKey):"，
// 老版是 "Public key:"，所以两种都认。
func realityKeys(bin string) (priv, pub string, err error) {
	out, err := exec.Command(bin, "x25519").Output()
	if err != nil {
		return "", "", fmt.Errorf("生成 REALITY 密钥失败: %w", err)
	}
	text := string(out)

	rePriv := regexp.MustCompile(`(?i)private\s*key:\s*(\S+)`)
	rePub := regexp.MustCompile(`(?i)(?:password\s*\(publickey\)|public\s*key):\s*(\S+)`)

	mp := rePriv.FindStringSubmatch(text)
	mb := rePub.FindStringSubmatch(text)
	if mp == nil || mb == nil {
		return "", "", fmt.Errorf("无法解析 xray x25519 输出: %s", strings.TrimSpace(text))
	}
	return mp[1], mb[1], nil
}

// randomShortID 生成 REALITY 的 shortId。
// 长度必须是偶数且不超过 16 个十六进制字符。
func randomShortID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "0123abcd"
	}
	return hex.EncodeToString(b)
}

// selfSignedCert 生成一张自签证书，用于没有真实域名时也能开 TLS。
//
// 走 openssl 而不是 Go 的 crypto/x509：证书要落成 Xray 能读的 PEM 文件，
// openssl 一条命令就够，省掉一大段编解码代码。
func selfSignedCert(dir, serverName string) (certFile, keyFile string, err error) {
	certDir := filepath.Join(dir, "certs")
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return "", "", err
	}
	base := filepath.Join(certDir, sanitizeTag(serverName))
	certFile, keyFile = base+".crt", base+".key"

	cmd := exec.Command("openssl", "req", "-x509", "-nodes",
		"-newkey", "rsa:2048",
		"-days", "3650",
		"-keyout", keyFile,
		"-out", certFile,
		"-subj", "/CN="+serverName,
		"-addext", "subjectAltName=DNS:"+serverName,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("生成自签证书失败: %s", trimOutput(out))
	}
	return certFile, keyFile, nil
}

// certFingerprint 算出证书的 SHA-256 指纹（十六进制）。
//
// Xray 26.x 移除了 allowInsecure，自签证书要靠 pinnedPeerCertSha256
// 让客户端固定信任这一张，所以生成分享链接时必须带上。
func certFingerprint(certFile string) (string, error) {
	der, err := exec.Command("openssl", "x509", "-in", certFile, "-outform", "der").Output()
	if err != nil {
		return "", fmt.Errorf("读取证书失败: %w", err)
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

// 支持的取值。集中在这里，前后端校验共用一份。
var (
	nativeNetworks   = map[string]bool{"tcp": true, "ws": true, "grpc": true, "httpupgrade": true, "xhttp": true}
	nativeSecurities = map[string]bool{"none": true, "tls": true, "reality": true}
)

// visionCapable 判断能不能用 xtls-rprx-vision。
//
// Vision 只在 VLESS + 裸 TCP + TLS/REALITY 下有效，其他组合 Xray 会拒绝启动。
func visionCapable(protocol, network, security string) bool {
	return protocol == "vless" && network == "tcp" && (security == "tls" || security == "reality")
}
