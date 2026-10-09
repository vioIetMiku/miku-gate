// Package tunnel contains deliberately unconnected VPN data-plane building
// blocks. Nothing in the Web API or executor IPC can start these yet.
package tunnel

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mikugate/internal/egress"
)

const MaxProfileBytes = 128 << 10
const Device = "mgvpn0"

var ErrProfile = errors.New("unsupported or unsafe OpenVPN runtime profile")
var peerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Endpoint is a literal IPv4 endpoint. Runtime selection never resolves a host
// name through the host resolver before bringing up the tunnel.
type Endpoint struct {
	IP        netip.Addr
	Port      uint16
	Transport string
}

// Profile keeps private key/certificate material out of ordinary JSON/log output.
// Source text is never forwarded to OpenVPN; only canonical fields are rendered.
type Profile struct {
	endpoint                       Endpoint
	ca, certificate, key, peerName string
}

func (p Profile) Endpoint() Endpoint { return p.endpoint }
func (p Profile) String() string     { return "OpenVPN runtime profile (credential material redacted)" }
func (p Profile) GoString() string   { return p.String() }

// Normalize applies public-endpoint policy in addition to strict syntax and TLS
// material checks. Public node provenance does not establish operator trust.
func Normalize(raw, expectedIP string) (Profile, error) {
	return normalize(raw, expectedIP, time.Now(), egress.PublicIPv4)
}
func normalize(raw, expectedIP string, now time.Time, allowEndpoint func(string) error) (Profile, error) {
	fail := func() (Profile, error) { return Profile{}, ErrProfile }
	if len(raw) == 0 || len(raw) > MaxProfileBytes {
		return fail()
	}
	expected, err := netip.ParseAddr(expectedIP)
	if err != nil || !expected.Is4() || expected.String() != expectedIP || allowEndpoint(expectedIP) != nil {
		return fail()
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	for _, c := range raw {
		if c == 0 || c == '\r' || c > 126 || (c < 32 && c != '\n' && c != '\t') {
			return fail()
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), MaxProfileBytes)
	values := map[string][]string{}
	blocks := map[string]string{}
	block := ""
	var content strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if block != "" {
			if line == "</"+block+">" {
				blocks[block] = content.String()
				content.Reset()
				block = ""
				continue
			}
			if strings.ContainsAny(line, "<>") {
				return fail()
			}
			content.WriteString(line)
			content.WriteByte('\n')
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if line == "<ca>" || line == "<cert>" || line == "<key>" {
			block = strings.Trim(line, "<>")
			if _, ok := blocks[block]; ok {
				return fail()
			}
			continue
		}
		fields := strings.Fields(line)
		key := fields[0]
		args := fields[1:]
		if _, ok := values[key]; ok {
			return fail()
		}
		if strings.ContainsAny(line, "\\\"'<>;") {
			return fail()
		}
		values[key] = args
		switch key {
		case "client", "nobind", "persist-key", "persist-tun", "auth-nocache":
			if len(args) != 0 {
				return fail()
			}
		case "dev":
			if len(args) != 1 || args[0] != "tun" {
				return fail()
			}
		case "proto":
			if len(args) != 1 || protocol(args[0]) == "" {
				return fail()
			}
		case "remote":
			if len(args) < 2 || len(args) > 3 || args[0] != expectedIP {
				return fail()
			}
			port, e := strconv.ParseUint(args[1], 10, 16)
			if e != nil || port == 0 || strconv.Itoa(int(port)) != args[1] {
				return fail()
			}
			if len(args) == 3 && protocol(args[2]) == "" {
				return fail()
			}
		case "remote-cert-tls":
			if len(args) != 1 || args[0] != "server" {
				return fail()
			}
		case "verify-x509-name":
			if len(args) != 2 || !peerNamePattern.MatchString(args[0]) || args[1] != "name" {
				return fail()
			}
		case "cipher", "data-ciphers-fallback":
			if len(args) != 1 || !aead(args[0]) {
				return fail()
			}
		case "data-ciphers":
			if len(args) != 1 {
				return fail()
			}
			for _, cipher := range strings.Split(args[0], ":") {
				if !aead(cipher) {
					return fail()
				}
			}
		case "auth":
			if len(args) != 1 || (args[0] != "SHA256" && args[0] != "SHA384" && args[0] != "SHA512") {
				return fail()
			}
		case "comp-lzo":
			if len(args) != 1 || args[0] != "no" {
				return fail()
			}
		case "resolv-retry":
			if len(args) != 1 || (args[0] != "infinite" && !integer(args[0], 0, 300)) {
				return fail()
			}
		case "verb", "mute", "connect-timeout", "connect-retry-max", "ping", "ping-restart", "reneg-sec", "route-delay":
			if len(args) != 1 || !integer(args[0], 0, 86400) {
				return fail()
			}
		case "connect-retry":
			if len(args) < 1 || len(args) > 2 {
				return fail()
			}
			for _, arg := range args {
				if !integer(arg, 1, 300) {
					return fail()
				}
			}
		case "redirect-gateway":
			for _, arg := range args {
				if arg != "def1" && arg != "bypass-dhcp" {
					return fail()
				}
			}
		case "setenv":
			if len(args) != 2 || args[0] != "CLIENT_CERT" || args[1] != "0" {
				return fail()
			}
		default:
			return fail() // Includes config/includes, plugins, scripts, paths, auth-user-pass and management.
		}
	}
	if scanner.Err() != nil || block != "" {
		return fail()
	}
	if _, ok := values["client"]; !ok {
		return fail()
	}
	if _, ok := values["dev"]; !ok {
		return fail()
	}
	remote, ok := values["remote"]
	if !ok {
		return fail()
	}
	transport := "udp4"
	if proto := values["proto"]; len(proto) == 1 {
		transport = protocol(proto[0])
	}
	if len(remote) == 3 {
		specified := protocol(remote[2])
		if values["proto"] != nil && specified != transport {
			return fail()
		}
		transport = specified
	}
	port, _ := strconv.Atoi(remote[1])
	ca, roots, err := normalizeCA(blocks["ca"], now)
	if err != nil {
		return fail()
	}
	profile := Profile{endpoint: Endpoint{IP: expected, Port: uint16(port), Transport: transport}, ca: ca}
	if name := values["verify-x509-name"]; len(name) == 2 {
		profile.peerName = name[0]
	}
	cert, key := blocks["cert"], blocks["key"]
	if (cert == "") != (key == "") {
		return fail()
	}
	if cert != "" {
		canonicalCert, canonicalKey, err := normalizeClientIdentity(cert, key, roots, now)
		if err != nil {
			return fail()
		}
		profile.certificate, profile.key = canonicalCert, canonicalKey
	}
	return profile, nil
}
func protocol(value string) string {
	switch value {
	case "udp", "udp4":
		return "udp4"
	case "tcp", "tcp4", "tcp-client", "tcp4-client":
		return "tcp4-client"
	}
	return ""
}
func aead(value string) bool {
	return value == "AES-256-GCM" || value == "AES-128-GCM" || value == "CHACHA20-POLY1305"
}
func integer(s string, low, high int) bool {
	v, err := strconv.Atoi(s)
	return err == nil && strconv.Itoa(v) == s && v >= low && v <= high
}
func strongKey(key any) bool {
	switch k := key.(type) {
	case *rsa.PublicKey:
		return k.N.BitLen() >= 2048
	case *ecdsa.PublicKey:
		return k.Curve.Params().BitSize >= 256
	}
	return false
}
func certs(raw string) ([]*x509.Certificate, error) {
	var result []*x509.Certificate
	for strings.TrimSpace(raw) != "" {
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "-----BEGIN CERTIFICATE-----") {
			return nil, ErrProfile
		}
		block, rest := pem.Decode([]byte(trimmed))
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(result) >= 8 {
			return nil, ErrProfile
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !strongKey(cert.PublicKey) || !strongSignature(cert.SignatureAlgorithm) {
			return nil, ErrProfile
		}
		result = append(result, cert)
		raw = string(rest)
	}
	if len(result) == 0 {
		return nil, ErrProfile
	}
	return result, nil
}
func normalizeCA(raw string, now time.Time) (string, *x509.CertPool, error) {
	values, err := certs(raw)
	if err != nil {
		return "", nil, err
	}
	pool := x509.NewCertPool()
	var out strings.Builder
	for _, cert := range values {
		if !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
			return "", nil, ErrProfile
		}
		// This initial format accepts explicit self-signed roots only, not a guessed chain.
		if cert.CheckSignatureFrom(cert) != nil {
			return "", nil, ErrProfile
		}
		pool.AddCert(cert)
		out.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
	}
	return out.String(), pool, nil
}
func normalizeClientIdentity(certText, keyText string, roots *x509.CertPool, now time.Time) (string, string, error) {
	values, err := certs(certText)
	if err != nil {
		return "", "", err
	}
	block, rest := pem.Decode([]byte(strings.TrimSpace(keyText)))
	if block == nil || len(block.Headers) != 0 || strings.TrimSpace(string(rest)) != "" || !strings.HasPrefix(strings.TrimSpace(keyText), "-----BEGIN ") {
		return "", "", ErrProfile
	}
	var key any
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return "", "", ErrProfile
	}
	if err != nil {
		return "", "", ErrProfile
	}
	canonical, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", ErrProfile
	}
	var out strings.Builder
	intermediates := x509.NewCertPool()
	for i, cert := range values {
		out.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
		if i > 0 {
			intermediates.AddCert(cert)
		}
	}
	encodedKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: canonical}))
	if _, err = tls.X509KeyPair([]byte(out.String()), []byte(encodedKey)); err != nil {
		return "", "", ErrProfile
	}
	leaf := values[0]
	if leaf.IsCA {
		return "", "", ErrProfile
	}
	hasClientRole := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientRole = true
		}
	}
	if !hasClientRole {
		return "", "", ErrProfile
	}
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return "", "", ErrProfile
	}
	return out.String(), encodedKey, nil
}

// render emits owned settings only. Neither source routes nor server-pushed
// routes/DNS reach the host. Address assignment is still scoped to a namespace.
func (p Profile) render() string {
	header := fmt.Sprintf(`client
proto %s
remote %s %d
dev %s
dev-type tun
nobind
resolv-retry 0
connect-retry 1 1
connect-retry-max 2
connect-timeout 5
tls-version-min 1.2
remote-cert-tls server
data-ciphers AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305
auth SHA256
allow-compression no
disable-dco
route-nopull
route-noexec
pull-filter ignore "route"
pull-filter ignore "redirect-gateway"
pull-filter ignore "dhcp-option"
pull-filter ignore "block-outside-dns"
pull-filter ignore "ping"
pull-filter reject "ifconfig-ipv6"
script-security 1
auth-nocache
persist-key
persist-tun
ping 2
ping-restart 6
verb 3
mute 5
`, p.endpoint.Transport, p.endpoint.IP, p.endpoint.Port, Device)
	if p.peerName != "" {
		header += "verify-x509-name " + p.peerName + " name\n"
	}
	header += "<ca>\n" + p.ca + "</ca>\n"
	if p.certificate != "" {
		header += "<cert>\n" + p.certificate + "</cert>\n<key>\n" + p.key + "</key>\n"
	}
	return header
}

func strongSignature(value x509.SignatureAlgorithm) bool {
	switch value {
	case x509.SHA256WithRSA, x509.SHA384WithRSA, x509.SHA512WithRSA, x509.SHA256WithRSAPSS, x509.SHA384WithRSAPSS, x509.SHA512WithRSAPSS, x509.ECDSAWithSHA256, x509.ECDSAWithSHA384, x509.ECDSAWithSHA512:
		return true
	}
	return false
}
