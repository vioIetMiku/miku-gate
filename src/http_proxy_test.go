package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPProxyAuthenticationAndTunnelFailure(t *testing.T) {
	for _, test := range []struct {
		auth        string
		want, calls int
	}{
		{"", 407, 0}, {base64.StdEncoding.EncodeToString([]byte("alice:wrong")), 407, 0},
		{base64.StdEncoding.EncodeToString([]byte("alice:correct")), 502, 1},
	} {
		client, server := net.Pipe()
		calls := 0
		done := make(chan struct{})
		go func() {
			serveProxy(server, &SocksCred{User: "alice", Pass: "correct"}, func(_, _ string) (net.Conn, error) { calls++; return nil, fmt.Errorf("tunnel down") })
			close(done)
		}()
		client.SetDeadline(time.Now().Add(2 * time.Second))
		header := ""
		if test.auth != "" {
			header = "Proxy-Authorization: Basic " + test.auth + "\r\n"
		}
		io.WriteString(client, "CONNECT target.invalid:443 HTTP/1.1\r\nHost: target.invalid:443\r\n"+header+"\r\n")
		response, err := http.ReadResponse(bufio.NewReader(client), nil)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		client.Close()
		<-done
		if response.StatusCode != test.want || calls != test.calls {
			t.Fatalf("status=%d calls=%d", response.StatusCode, calls)
		}
	}
}

func TestHTTPProxyForwardUsesOnlyProvidedDialer(t *testing.T) {
	client, server := net.Pipe()
	remote, origin := net.Pipe()
	done := make(chan struct{})
	checked := make(chan error, 1)
	go func() {
		defer origin.Close()
		request, err := http.ReadRequest(bufio.NewReader(origin))
		if err == nil && request.Header.Get("Proxy-Authorization") != "" {
			err = fmt.Errorf("proxy credential forwarded")
		}
		checked <- err
		if err == nil {
			io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\nConnection: close\r\n\r\nhello")
		}
	}()
	go func() {
		serveProxy(server, &SocksCred{User: "alice", Pass: "correct"}, func(network, address string) (net.Conn, error) {
			if network != "tcp" || address != "target.invalid:80" {
				return nil, fmt.Errorf("unexpected route")
			}
			return remote, nil
		})
		close(done)
	}()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	token := base64.StdEncoding.EncodeToString([]byte("alice:correct"))
	io.WriteString(client, "GET http://target.invalid/ HTTP/1.1\r\nHost: target.invalid\r\nProxy-Authorization: Basic "+token+"\r\nConnection: close\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	client.Close()
	<-done
	if err != nil || response.StatusCode != 200 || string(body) != "hello" {
		t.Fatalf("forward response %v %s", err, body)
	}
	if err := <-checked; err != nil {
		t.Fatal(err)
	}
}

func TestHTTPProxyCONNECTPreservesBufferedPayload(t *testing.T) {
	client, server := net.Pipe()
	remote, origin := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer origin.Close()
		raw := make([]byte, 4)
		if _, err := io.ReadFull(origin, raw); err == nil {
			origin.Write(raw)
		}
	}()
	go func() {
		serveProxy(server, &SocksCred{User: "alice", Pass: "correct"}, func(_, _ string) (net.Conn, error) { return remote, nil })
		close(done)
	}()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	token := base64.StdEncoding.EncodeToString([]byte("alice:correct"))
	io.WriteString(client, "CONNECT target.invalid:443 HTTP/1.1\r\nHost: target.invalid:443\r\nProxy-Authorization: Basic "+token+"\r\n\r\nping")
	reader := bufio.NewReader(client)
	line, err := reader.ReadString('\n')
	if err != nil || !strings.Contains(line, "200") {
		t.Fatal(line, err)
	}
	for {
		line, err = reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	raw := make([]byte, 4)
	if _, err = io.ReadFull(reader, raw); err != nil || string(raw) != "ping" {
		t.Fatal(string(raw), err)
	}
	client.Close()
	<-done
}
