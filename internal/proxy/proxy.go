// Package proxy implements a bounded HTTP/SOCKS5 TCP protocol frontend.
// It never selects a system route or resolves DNS. A verified tunnel dialer
// must be injected before a listener can be used; nil dialers fail closed.
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DialFunc func(context.Context, string, string) (net.Conn, error)
type Server struct {
	Dial           DialFunc
	Authenticate   func(string, string) bool
	MaxConnections int
}

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	if s.Dial == nil || s.Authenticate == nil {
		return errors.New("verified dialer and authentication are required")
	}
	if ln == nil {
		return errors.New("listener is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); ln.Close(); workers.Wait() }()
	limit := s.MaxConnections
	if limit < 1 {
		limit = 128
	}
	slots := make(chan struct{}, limit)
	stop := context.AfterFunc(ctx, func() { ln.Close() })
	defer stop()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			workers.Add(1)
			go func() { defer workers.Done(); defer func() { <-slots }(); s.Handle(ctx, conn) }()
		default:
			conn.Close()
		}
	}
}
func (s *Server) Handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	if s.Dial == nil || s.Authenticate == nil {
		return
	}
	c.SetDeadline(time.Now().Add(15 * time.Second))
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	r := bufio.NewReaderSize(c, 4096)
	first, err := r.Peek(1)
	if err != nil {
		return
	}
	if first[0] == 5 {
		s.socks(ctx, c, r)
	} else {
		s.http(ctx, c, r)
	}
}
func (s *Server) connect(ctx context.Context, host, port string) (net.Conn, error) {
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "\x00\r\n /\\") || strings.Contains(host, ":") {
		return nil, errors.New("invalid or IPv6 destination")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, errors.New("invalid destination port")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.Dial(ctx, "tcp4", net.JoinHostPort(host, port))
}
func (s *Server) socks(ctx context.Context, c net.Conn, r *bufio.Reader) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(r, methods); err != nil {
		return
	}
	if !bytes.Contains(methods, []byte{2}) {
		c.Write([]byte{5, 255})
		return
	}
	if _, err := c.Write([]byte{5, 2}); err != nil {
		return
	}
	version, err := r.ReadByte()
	if err != nil || version != 1 {
		return
	}
	readString := func() (string, error) {
		n, e := r.ReadByte()
		if e != nil || n == 0 {
			return "", errors.New("invalid credentials")
		}
		b := make([]byte, int(n))
		_, e = io.ReadFull(r, b)
		return string(b), e
	}
	username, err := readString()
	if err != nil {
		return
	}
	password, err := readString()
	if err != nil {
		return
	}
	if !s.Authenticate(username, password) {
		c.Write([]byte{1, 1})
		return
	}
	c.Write([]byte{1, 0})
	var req [4]byte
	if _, err = io.ReadFull(r, req[:]); err != nil || req[0] != 5 || req[2] != 0 {
		return
	}
	reply := func(code byte) { c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0}) }
	if req[1] != 1 {
		reply(7)
		return
	}
	var host string
	switch req[3] {
	case 1:
		var ip [4]byte
		if _, err = io.ReadFull(r, ip[:]); err != nil {
			return
		}
		host = net.IP(ip[:]).String()
	case 3:
		host, err = readString()
		if err != nil {
			return
		}
	default:
		reply(8)
		return
	}
	var port [2]byte
	if _, err = io.ReadFull(r, port[:]); err != nil {
		return
	}
	up, err := s.connect(ctx, host, strconv.Itoa(int(port[0])<<8|int(port[1])))
	if err != nil {
		reply(4)
		return
	}
	defer up.Close()
	reply(0)
	relay(ctx, c, r, up)
}
func (s *Server) http(ctx context.Context, c net.Conn, r *bufio.Reader) {
	header := make([]byte, 0, 1024)
	for len(header) < 16*1024 {
		b, err := r.ReadByte()
		if err != nil {
			return
		}
		header = append(header, b)
		if bytes.HasSuffix(header, []byte("\r\n\r\n")) {
			break
		}
	}
	if !bytes.HasSuffix(header, []byte("\r\n\r\n")) {
		httpError(c, 431, "Request Header Fields Too Large")
		return
	}
	reqReader := bufio.NewReader(io.MultiReader(bytes.NewReader(header), r))
	req, err := http.ReadRequest(reqReader)
	if err != nil {
		httpError(c, 400, "Bad Request")
		return
	}
	defer req.Body.Close()
	auth := req.Header.Get("Proxy-Authorization")
	authReq := &http.Request{Header: http.Header{"Authorization": []string{auth}}}
	user, pass, ok := authReq.BasicAuth()
	if !ok || !s.Authenticate(user, pass) {
		io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"Miku Gate\"\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
		return
	}
	var host, port string
	if req.Method == http.MethodConnect {
		if req.ContentLength > 0 || len(req.TransferEncoding) != 0 {
			httpError(c, 400, "CONNECT Body Not Supported")
			return
		}
		host, port, err = net.SplitHostPort(req.Host)
		if err != nil {
			httpError(c, 400, "Bad CONNECT Target")
			return
		}
	} else {
		if req.URL.Scheme != "http" || req.URL.User != nil || req.URL.Host == "" {
			httpError(c, 400, "Use HTTP absolute URI or CONNECT")
			return
		}
		host = req.URL.Hostname()
		port = req.URL.Port()
		if port == "" {
			port = "80"
		}
	}
	up, err := s.connect(ctx, host, port)
	if err != nil {
		httpError(c, 502, "Tunnel Unavailable")
		return
	}
	defer up.Close()
	stopUp := context.AfterFunc(ctx, func() { up.Close() })
	defer stopUp()
	if req.Method == http.MethodConnect {
		io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
		relay(ctx, c, reqReader, up)
		return
	}
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Authorization-Proxy")
	req.Close = true
	req.RequestURI = ""
	req.URL.Scheme = ""
	req.URL.Host = ""
	c.SetDeadline(time.Now().Add(5 * time.Minute))
	up.SetDeadline(time.Now().Add(5 * time.Minute))
	if err = req.Write(up); err != nil {
		return
	}
	// One ordinary HTTP request per authenticated connection. Do not forward
	// pipelined bytes (and a second Proxy-Authorization header) to the target.
	_, _ = io.Copy(c, up)
}
func httpError(c net.Conn, status int, text string) {
	fmt.Fprintf(c, "HTTP/1.1 %d %s\r\nConnection: close\r\nContent-Length: 0\r\n\r\n", status, text)
}

type activityReader struct {
	io.Reader
	touch func()
}

func (r activityReader) Read(p []byte) (int, error) { r.touch(); return r.Reader.Read(p) }

type activityWriter struct {
	io.Writer
	touch func()
}

func (w activityWriter) Write(p []byte) (int, error) { w.touch(); return w.Writer.Write(p) }
func relay(ctx context.Context, client net.Conn, reader io.Reader, up net.Conn) {
	touch := func() {
		deadline := time.Now().Add(5 * time.Minute)
		client.SetDeadline(deadline)
		up.SetDeadline(deadline)
	}
	touch()
	stop := context.AfterFunc(ctx, func() { client.Close(); up.Close() })
	defer stop()
	done := make(chan struct{}, 2)
	copyStream := func(dst net.Conn, src io.Reader) {
		_, err := io.CopyBuffer(activityWriter{Writer: dst, touch: touch}, activityReader{Reader: src, touch: touch}, make([]byte, 32*1024))
		if err == nil {
			if half, ok := dst.(interface{ CloseWrite() error }); ok {
				err = half.CloseWrite()
			} else {
				err = io.ErrClosedPipe
			}
		}
		if err != nil {
			client.Close()
			up.Close()
		}
		done <- struct{}{}
	}
	go copyStream(up, reader)
	go copyStream(client, up)
	<-done
	<-done
	client.Close()
	up.Close()
}
