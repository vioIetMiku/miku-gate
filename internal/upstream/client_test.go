package upstream

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"mikugate/internal/domain"
	"mikugate/internal/proxy"
)

func labCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "private upstream test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"cloudflare-dns.com", "dns.google", "proxy.example"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret}))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return cert, roots
}
func serveProxy(t *testing.T, server *proxy.Server, cert *tls.Certificate) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if cert != nil {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{*cert}, MinVersion: tls.VersionTLS12})
	}
	address := ln.Addr().String()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, ln) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			ln.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(3 * time.Second):
				t.Error("proxy fixture failed to drain")
			}
		})
	}
	t.Cleanup(stop)
	return address, stop
}

type fixture struct {
	client        *Client
	stop          func()
	destinations  *atomic.Int32
	dnsCalls      *atomic.Int32
	resolverCalls *atomic.Int32
}

func newFixture(t *testing.T, kind string) fixture {
	t.Helper()
	cert, roots := labCertificate(t)
	var dnsCalls, targets, resolverCalls atomic.Int32
	dnsServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dnsCalls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/dns-query" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("invalid DoH request")
			w.WriteHeader(400)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		if err != nil {
			t.Error(err)
			return
		}
		var query dnsmessage.Message
		if query.Unpack(raw) != nil || len(query.Questions) != 1 {
			w.WriteHeader(400)
			return
		}
		name := query.Questions[0].Name.String()
		if name == "timeout.example." {
			<-r.Context().Done()
			return
		}
		if name == "redirect.example." {
			http.Redirect(w, r, "https://dns.google/dns-query", 302)
			return
		}
		response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true}, Questions: query.Questions}
		answer := func(ip string) dnsmessage.Resource {
			return dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: netip.MustParseAddr(ip).As4()}}
		}
		response.Answers = []dnsmessage.Resource{answer("93.184.216.34")}
		if name == "private.example." {
			response.Answers = append(response.Answers, answer("10.0.0.1"))
		}
		if name == "mismatch.example." {
			response.ID++
		}
		if name == "truncated.example." {
			response.Truncated = true
		}
		packed, err := response.Pack()
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(packed)
	}))
	dnsServer.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	dnsServer.StartTLS()
	t.Cleanup(dnsServer.Close)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to destination")
		}
		io.WriteString(w, "via-configured-upstream")
	}))
	t.Cleanup(target.Close)
	var tlsProxy *tls.Certificate
	if kind == "https" {
		tlsProxy = &cert
	}
	endpoint, stop := serveProxy(t, &proxy.Server{Authenticate: func(u, p string) bool { return u == "up-user" && p == "up-pass" }, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		mapped := ""
		switch address {
		case "1.1.1.1:443", "8.8.8.8:443":
			mapped = dnsServer.Listener.Addr().String()
		case "93.184.216.34:8080":
			targets.Add(1)
			mapped = target.Listener.Addr().String()
		default:
			t.Errorf("unexpected/unresolved destination sent to upstream: %s", address)
			return nil, ErrDestination
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp4", mapped)
	}}, tlsProxy)
	host, port, _ := net.SplitHostPort(endpoint)
	number, _ := strconv.Atoi(port)
	config := domain.UpstreamConfig{Kind: kind, Host: host, Port: number, Username: "up-user", Password: "up-pass", AllowLoopback: true}
	if kind == "https" {
		config.TLSName = "proxy.example"
	}
	client, err := newClient(t.Context(), config, options{roots: roots, resolve: func(context.Context, string) ([]netip.Addr, error) {
		resolverCalls.Add(1)
		return nil, errors.New("host resolver forbidden for business DNS")
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return fixture{client: client, stop: stop, destinations: &targets, dnsCalls: &dnsCalls, resolverCalls: &resolverCalls}
}
func readTarget(t *testing.T, conn net.Conn) {
	t.Helper()
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: business.example\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(body) != "via-configured-upstream" {
		t.Fatal("wrong business response", err)
	}
}
func TestProxyUpstreamsUseRemoteDoHAndNumericTargets(t *testing.T) {
	for _, kind := range []string{"socks5", "http", "https"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, kind)
			if err := f.client.Probe(t.Context()); err != nil {
				t.Fatal("real proxy/TLS/DoH probe failed", err)
			}
			c, err := f.client.DialContext(t.Context(), "tcp4", "business.example:8080")
			if err != nil {
				t.Fatal(err)
			}
			readTarget(t, c)
			c, err = f.client.DialContext(t.Context(), "tcp4", "93.184.216.34:8080")
			if err != nil {
				t.Fatal(err)
			}
			readTarget(t, c)
			if f.destinations.Load() != 2 || f.dnsCalls.Load() < 2 || f.resolverCalls.Load() != 0 {
				t.Fatal("wrong proxy/DNS path")
			}
		})
	}
}
func TestBadDNSPrivateTargetsAndProxyLossNeverFallBack(t *testing.T) {
	f := newFixture(t, "socks5")
	for _, address := range []string{"127.0.0.1:80", "169.254.169.254:80", "[::1]:80", "[::ffff:8.8.8.8]:80", "127.1:80", "private.example:8080", "mismatch.example:8080", "truncated.example:8080", "redirect.example:8080"} {
		if conn, err := f.client.DialContext(t.Context(), "tcp4", address); err == nil {
			conn.Close()
			t.Fatal("forbidden target or DNS answer accepted", address)
		}
	}
	timeout, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if conn, err := f.client.DialContext(timeout, "tcp4", "timeout.example:8080"); err == nil {
		conn.Close()
		t.Fatal("DNS timeout unexpectedly connected")
	}
	if f.destinations.Load() != 0 || f.resolverCalls.Load() != 0 {
		t.Fatal("forbidden target reached business/host resolver")
	}
	f.stop()
	if conn, err := f.client.DialContext(t.Context(), "tcp4", "93.184.216.34:8080"); err == nil {
		conn.Close()
		t.Fatal("direct fallback after upstream loss")
	}
}
func TestRealMixedFrontendThroughConfiguredUpstream(t *testing.T) {
	f := newFixture(t, "http")
	endpoint, _ := serveProxy(t, &proxy.Server{Dial: f.client.DialContext, Authenticate: func(u, p string) bool { return u == "client" && p == "client-secret" }}, nil)
	conn, err := net.Dial("tcp4", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	io.WriteString(conn, "GET http://business.example:8080/ HTTP/1.1\r\nHost: business.example:8080\r\nProxy-Authorization: Basic Y2xpZW50OmNsaWVudC1zZWNyZXQ=\r\nConnection: close\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(body) != "via-configured-upstream" {
		t.Fatal("full authenticated proxy path failed", err)
	}
	if f.destinations.Load() != 1 || f.resolverCalls.Load() != 0 {
		t.Fatal("wrong full path")
	}
}
func TestCONNECTBufferedDataAndAuthDowngrade(t *testing.T) {
	t.Run("http-buffer", func(t *testing.T) {
		left, right := net.Pipe()
		defer left.Close()
		defer right.Close()
		left.SetDeadline(time.Now().Add(time.Second))
		right.SetDeadline(time.Now().Add(time.Second))
		go func() {
			r := bufio.NewReader(right)
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if line == "\r\n" {
					break
				}
			}
			io.WriteString(right, "HTTP/1.1 200 Connection Established\r\n\r\nbanner")
		}()
		conn, err := httpConnect(left, domain.UpstreamConfig{}, "93.184.216.34:80")
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 6)
		if _, err = io.ReadFull(conn, buf); err != nil || string(buf) != "banner" {
			t.Fatal("lost buffered data", err)
		}
	})
	t.Run("socks-downgrade", func(t *testing.T) {
		left, right := net.Pipe()
		defer left.Close()
		defer right.Close()
		left.SetDeadline(time.Now().Add(time.Second))
		go func() { var request [3]byte; io.ReadFull(right, request[:]); right.Write([]byte{5, 0}) }()
		if _, err := socksConnect(left, domain.UpstreamConfig{Username: "u", Password: "p"}, "93.184.216.34", 80); !errors.Is(err, ErrAuthentication) {
			t.Fatal("authentication downgrade accepted", err)
		}
	})
}
func TestEndpointRebindingAndTLSMismatchRefused(t *testing.T) {
	config := domain.UpstreamConfig{Kind: "https", Host: "proxy.example", Port: 443, AllowLoopback: true}
	for _, ip := range []string{"127.0.0.1", "169.254.169.254", "10.0.0.1"} {
		_, err := newClient(t.Context(), config, options{resolve: func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(ip)}, nil }})
		if !errors.Is(err, ErrEndpoint) {
			t.Fatal("DNS rebinding accepted", ip, err)
		}
	}
	f := newFixture(t, "https")
	f.client.config.TLSName = "not-the-proxy.example"
	if conn, err := f.client.DialContext(t.Context(), "tcp4", "93.184.216.34:8080"); !errors.Is(err, ErrTLS) {
		if conn != nil {
			conn.Close()
		}
		t.Fatal("TLS identity mismatch accepted", err)
	}
	if f.destinations.Load() != 0 {
		t.Fatal("business target reached with invalid TLS")
	}
}
func TestClientCloseRevokesExistingConnections(t *testing.T) {
	f := newFixture(t, "socks5")
	conn, err := f.client.DialContext(t.Context(), "tcp4", "93.184.216.34:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	f.client.Close()
	if _, err = conn.Write([]byte("x")); err == nil {
		t.Fatal("old connection survived close")
	}
	f.client.mu.Lock()
	count := len(f.client.connections)
	f.client.mu.Unlock()
	if count != 0 {
		t.Fatal("connections retained after close")
	}
}

// Keep response-size and parser edge cases in direct protocol tests as well.
func TestSOCKSResponsePreservesBanner(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	left.SetDeadline(time.Now().Add(time.Second))
	go func() {
		var greet [3]byte
		io.ReadFull(right, greet[:])
		right.Write([]byte{5, 0})
		var req [10]byte
		io.ReadFull(right, req[:])
		if binary.BigEndian.Uint16(req[8:]) != 80 {
			return
		}
		right.Write(append([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}, []byte("hi")...))
	}()
	conn, err := socksConnect(left, domain.UpstreamConfig{}, "93.184.216.34", 80)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 2)
	if _, err = io.ReadFull(conn, data); err != nil || string(data) != "hi" {
		t.Fatal("SOCKS buffered bytes lost", err)
	}
}
