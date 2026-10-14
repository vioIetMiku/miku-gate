package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type bufferedProxyConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedProxyConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// One authenticated port accepts SOCKS5 and HTTP. Every connection uses the
// selected tunnel's dialer; transport failure never uses a host-network dialer.
func serveProxy(c net.Conn, credential *SocksCred, dial func(string, string) (net.Conn, error)) {
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	r := bufio.NewReaderSize(c, 64<<10)
	first, err := r.Peek(1)
	if err != nil {
		c.Close()
		return
	}
	if first[0] == socksVer5 {
		serveSocks(&bufferedProxyConn{Conn: c, r: r}, credential, dial)
		return
	}
	serveHTTPProxy(c, r, credential, dial)
}

func proxyAuthentication(request *http.Request, credential *SocksCred) bool {
	if credential == nil || credential.User == "" || credential.Pass == "" {
		return false
	}
	scheme, encoded, ok := strings.Cut(request.Header.Get("Proxy-Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Basic") || len(encoded) > 1024 {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	user, password, ok := strings.Cut(string(raw), ":")
	return ok && subtle.ConstantTimeCompare([]byte(user), []byte(credential.User)) == 1 && subtle.ConstantTimeCompare([]byte(password), []byte(credential.Pass)) == 1
}

func readProxyRequest(reader *bufio.Reader) (*http.Request, *bufio.Reader, error) {
	var header bytes.Buffer
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil || header.Len()+len(line) > 64<<10 {
			return nil, reader, fmt.Errorf("HTTP请求头超限或不完整")
		}
		header.Write(line)
		if bytes.Equal(line, []byte("\r\n")) || bytes.Equal(line, []byte("\n")) {
			break
		}
	}
	next := bufio.NewReader(io.MultiReader(bytes.NewReader(header.Bytes()), reader))
	request, err := http.ReadRequest(next)
	return request, next, err
}

func serveHTTPProxy(client net.Conn, reader *bufio.Reader, credential *SocksCred, dial func(string, string) (net.Conn, error)) {
	defer client.Close()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second, DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
		return dial(network, address)
	}}
	defer transport.CloseIdleConnections()
	for count := 0; count < 32; count++ {
		_ = client.SetDeadline(time.Now().Add(30 * time.Second))
		request, next, err := readProxyRequest(reader)
		reader = next
		if err != nil {
			return
		}
		if !proxyAuthentication(request, credential) {
			io.WriteString(client, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"Miku\"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			return
		}
		if request.Method == http.MethodConnect {
			address := request.Host
			if _, _, err = net.SplitHostPort(address); err != nil {
				io.WriteString(client, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
				return
			}
			remote, err := dial("tcp", address)
			if err != nil {
				io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
				return
			}
			defer remote.Close()
			if _, err = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
				return
			}
			_ = client.SetDeadline(time.Time{})
			relay(&bufferedProxyConn{Conn: client, r: reader}, remote)
			return
		}
		if request.URL.Scheme != "http" || request.URL.Host == "" {
			io.WriteString(client, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			return
		}
		request.RequestURI = ""
		for _, token := range strings.Split(request.Header.Get("Connection"), ",") {
			request.Header.Del(strings.TrimSpace(token))
		}
		for _, name := range []string{"Proxy-Authorization", "Proxy-Connection", "Connection", "Keep-Alive", "TE", "Trailer", "Upgrade"} {
			request.Header.Del(name)
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			return
		}
		_ = client.SetDeadline(time.Time{})
		err = response.Write(client)
		response.Body.Close()
		if err != nil || request.Close || response.Close {
			return
		}
	}
}
