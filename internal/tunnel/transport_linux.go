//go:build linux

package tunnel

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"mikugate/internal/streamio"
)

// transportProxy exists only inside the isolated namespace. Its sole external
// socket was connected to the selected VPN peer by the parent before exec. It
// cannot open another host-network connection or proxy arbitrary destinations.
type transportProxy struct {
	listener net.Listener
	outer    net.Conn
	endpoint Endpoint
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
}

func newTransportProxy(ctx context.Context, outer net.Conn, endpoint Endpoint) (*transportProxy, error) {
	if outer == nil || endpoint.Port == 0 || !endpoint.IP.Is4() {
		return nil, ErrProfile
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	proxy := &transportProxy{listener: listener, outer: outer, endpoint: endpoint, ctx: life, cancel: cancel, done: make(chan struct{})}
	go proxy.run()
	return proxy, nil
}
func (p *transportProxy) Port() int { return p.listener.Addr().(*net.TCPAddr).Port }
func (p *transportProxy) Close() {
	p.once.Do(func() { p.cancel(); p.listener.Close(); p.outer.Close() })
	<-p.done
}
func (p *transportProxy) run() {
	defer close(p.done)
	defer p.outer.Close()
	defer p.listener.Close()
	stop := context.AfterFunc(p.ctx, func() { p.listener.Close(); p.outer.Close() })
	defer stop()
	conn, err := p.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	// Each immutable VPN session uses one SOCKS control connection. Any failure
	// tears down the entire session; no implicit reconnect to a different peer.
	p.listener.Close()
	stopConn := context.AfterFunc(p.ctx, func() { conn.Close() })
	defer stopConn()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	var greeting [2]byte
	if _, err = io.ReadFull(conn, greeting[:]); err != nil || greeting[0] != 5 || greeting[1] == 0 || greeting[1] > 8 {
		return
	}
	methods := make([]byte, int(greeting[1]))
	if _, err = io.ReadFull(conn, methods); err != nil {
		return
	}
	anonymous := false
	for _, method := range methods {
		if method == 0 {
			anonymous = true
		}
	}
	if !anonymous {
		return
	}
	if _, err = conn.Write([]byte{5, 0}); err != nil {
		return
	}
	var request [10]byte
	if _, err = io.ReadFull(conn, request[:]); err != nil || request[0] != 5 || request[2] != 0 || request[3] != 1 {
		return
	}
	expected := p.endpoint.IP.As4()
	if p.endpoint.Transport == "tcp4-client" {
		if request[1] != 1 || string(request[4:8]) != string(expected[:]) || binary.BigEndian.Uint16(request[8:]) != p.endpoint.Port {
			return
		}
		if _, err = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
			return
		}
		conn.SetDeadline(time.Time{})
		streamio.Bridge(p.ctx, conn, p.outer)
		return
	}
	if p.endpoint.Transport != "udp4" || request[1] != 3 {
		return
	}
	// RFC1928 UDP ASSOCIATE's requested address is not the final peer. Never use
	// it for routing: the only peer remains endpoint, and the first local sender
	// address is pinned for this one OpenVPN process/session.
	if !(request[4] == 0 && request[5] == 0 && request[6] == 0 && request[7] == 0) && !(request[4] == 127 && request[5] == 0 && request[6] == 0 && request[7] == 1) {
		return
	}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return
	}
	defer udp.Close()
	response := []byte{5, 0, 0, 1, 127, 0, 0, 1}
	response = binary.BigEndian.AppendUint16(response, uint16(udp.LocalAddr().(*net.UDPAddr).Port))
	if _, err = conn.Write(response); err != nil {
		return
	}
	conn.SetDeadline(time.Time{})
	life, cancel := context.WithCancel(p.ctx)
	defer cancel()
	stopUDP := context.AfterFunc(life, func() { udp.Close(); p.outer.Close(); conn.Close() })
	defer stopUDP()
	var mu sync.Mutex
	var sender *net.UDPAddr
	wait := make(chan struct{}, 3)
	go func() { defer func() { cancel(); wait <- struct{}{} }(); var b [1]byte; conn.Read(b[:]) }()
	go func() {
		defer func() { cancel(); wait <- struct{}{} }()
		packet := make([]byte, 65536)
		for {
			n, from, err := udp.ReadFromUDP(packet)
			if err != nil {
				return
			}
			if !from.IP.IsLoopback() || n < 10 || packet[0] != 0 || packet[1] != 0 || packet[2] != 0 || packet[3] != 1 || string(packet[4:8]) != string(expected[:]) || binary.BigEndian.Uint16(packet[8:10]) != p.endpoint.Port {
				continue
			}
			mu.Lock()
			if sender == nil {
				sender = from
			}
			same := sender.String() == from.String()
			mu.Unlock()
			if !same {
				continue
			}
			if n == 10 {
				continue
			}
			if _, err = p.outer.Write(packet[10:n]); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { cancel(); wait <- struct{}{} }()
		packet := make([]byte, 65536)
		header := []byte{0, 0, 0, 1}
		header = append(header, expected[:]...)
		header = binary.BigEndian.AppendUint16(header, p.endpoint.Port)
		for {
			n, err := p.outer.Read(packet[10:])
			if err != nil {
				return
			}
			mu.Lock()
			target := sender
			mu.Unlock()
			if target == nil {
				continue
			}
			copy(packet[:10], header)
			if _, err = udp.WriteToUDP(packet[:n+10], target); err != nil && !errors.Is(err, net.ErrClosed) {
				return
			}
		}
	}()
	<-wait
	<-wait
	<-wait
}
