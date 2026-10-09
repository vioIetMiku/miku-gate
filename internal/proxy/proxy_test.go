package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testConn(t *testing.T, s *Server) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(func() { cancel(); client.Close() })
	client.SetDeadline(time.Now().Add(2 * time.Second))
	go s.Handle(ctx, server)
	return client
}
func echoDial(ctx context.Context, network, address string) (net.Conn, error) {
	client, server := net.Pipe()
	go func() { defer server.Close(); io.Copy(server, server) }()
	return client, nil
}
func TestHTTPConnectPreservesPayload(t *testing.T) {
	s := &Server{Dial: echoDial, Authenticate: func(u, p string) bool { return u == "alice" && p == "secret" }}
	c := testConn(t, s)
	auth := base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	go io.WriteString(c, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic "+auth+"\r\n\r\npayload")
	r := bufio.NewReader(c)
	resp, err := http.ReadResponse(r, &http.Request{Method: "CONNECT"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	data := make([]byte, 7)
	if _, err = io.ReadFull(r, data); err != nil || string(data) != "payload" {
		t.Fatalf("buffered payload lost: %s %v", data, err)
	}
}
func TestHTTPAuthBeforeDial(t *testing.T) {
	var calls atomic.Int32
	s := &Server{Dial: func(ctx context.Context, n, a string) (net.Conn, error) { calls.Add(1); return echoDial(ctx, n, a) }, Authenticate: func(u, p string) bool { return false }}
	c := testConn(t, s)
	go io.WriteString(c, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	r := bufio.NewReader(c)
	line, err := r.ReadString('\n')
	if err != nil || !strings.Contains(line, "407") {
		t.Fatalf("%s %v", line, err)
	}
	if calls.Load() != 0 {
		t.Fatal("dial before authentication")
	}
}
func TestSOCKS5Connect(t *testing.T) {
	s := &Server{Dial: echoDial, Authenticate: func(u, p string) bool { return u == "u" && p == "p" }}
	c := testConn(t, s)
	exchange := func(send, want []byte) {
		t.Helper()
		if _, err := c.Write(send); err != nil {
			t.Fatal(err)
		}
		reply := make([]byte, len(want))
		if _, err := io.ReadFull(c, reply); err != nil {
			t.Fatal(err)
		}
		if string(reply) != string(want) {
			t.Fatalf("%v != %v", reply, want)
		}
	}
	exchange([]byte{5, 1, 2}, []byte{5, 2})
	exchange([]byte{1, 1, 'u', 1, 'p'}, []byte{1, 0})
	exchange([]byte{5, 1, 0, 3, 11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm', 1, 187}, []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	exchange([]byte("hello"), []byte("hello"))
}
func TestSOCKS5RejectsUnauthenticated(t *testing.T) {
	s := &Server{Dial: echoDial, Authenticate: func(u, p string) bool { return true }}
	c := testConn(t, s)
	c.Write([]byte{5, 1, 0})
	var data [2]byte
	if _, err := io.ReadFull(c, data[:]); err != nil {
		t.Fatal(err)
	}
	if data != [2]byte{5, 255} {
		t.Fatal("anonymous auth accepted")
	}
}
func TestMissingDialerCannotServe(t *testing.T) {
	s := &Server{}
	if err := s.Serve(context.Background(), nil); err == nil {
		t.Fatal("missing dialer accepted")
	}
}
func TestHTTPForwardStripsCredentials(t *testing.T) {
	captured := make(chan string, 1)
	s := &Server{Authenticate: func(u, p string) bool { return u == "u" && p == "p" }, Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		left, right := net.Pipe()
		go func() {
			defer right.Close()
			req, err := http.ReadRequest(bufio.NewReader(right))
			if err != nil {
				captured <- "parse failed"
				return
			}
			captured <- req.Header.Get("Proxy-Authorization")
			io.WriteString(right, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		}()
		return left, nil
	}}
	c := testConn(t, s)
	go io.WriteString(c, "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nProxy-Authorization: Basic dTpw\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: "GET"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", resp, err)
	}
	defer resp.Body.Close()
	if got := <-captured; got != "" {
		t.Fatal("credentials leaked to upstream")
	}
}

func TestHTTPRejectsCONNECTBody(t *testing.T) {
	var calls atomic.Int32
	s := &Server{Authenticate: func(u, p string) bool { return true }, Dial: func(ctx context.Context, n, a string) (net.Conn, error) { calls.Add(1); return echoDial(ctx, n, a) }}
	c := testConn(t, s)
	go io.WriteString(c, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic dTpw\r\nContent-Length: 2\r\n\r\nok")
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || !strings.Contains(line, "400") {
		t.Fatalf("%s %v", line, err)
	}
	if calls.Load() != 0 {
		t.Fatal("ambiguous CONNECT request dialed upstream")
	}
}

func TestTCPRelayPreservesHalfClose(t *testing.T) {
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		conn, err := target.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		payload, err := io.ReadAll(conn)
		if err == nil {
			conn.Write(append([]byte("reply:"), payload...))
		}
	}()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	s := Server{Authenticate: func(u, p string) bool { return u == "u" && p == "p" }, Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", target.Addr().String())
	}}
	go func() { done <- s.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("relay did not drain")
		}
	})
	conn, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic dTpw\r\n\r\n")
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil || response.StatusCode != 200 {
		t.Fatal("CONNECT failed", err)
	}
	conn.Write([]byte("request"))
	if err = conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil || string(payload) != "reply:request" {
		t.Fatal("half-close discarded the server response", string(payload), err)
	}
}
func TestHTTPForwardDoesNotLeakPipelinedProxyCredentials(t *testing.T) {
	observed := make(chan bool, 1)
	s := &Server{Authenticate: func(u, p string) bool { return u == "u" && p == "p" }, Dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		left, right := net.Pipe()
		go func() {
			defer right.Close()
			reader := bufio.NewReader(right)
			req, err := http.ReadRequest(reader)
			if err != nil {
				observed <- true
				return
			}
			if req.Header.Get("Proxy-Authorization") != "" {
				observed <- true
				return
			}
			io.WriteString(right, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			right.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, err = reader.ReadByte()
			observed <- err == nil
		}()
		return left, nil
	}}
	conn := testConn(t, s)
	go io.WriteString(conn, "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nProxy-Authorization: Basic dTpw\r\n\r\nGET http://example.com/second HTTP/1.1\r\nHost: example.com\r\nProxy-Authorization: Basic dTpw\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if <-observed {
		t.Fatal("pipelined proxy authorization leaked to the destination")
	}
}
