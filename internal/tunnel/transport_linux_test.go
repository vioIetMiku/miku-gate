//go:build linux

package tunnel

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestFixedTCPTransportDoesNotRouteArbitraryDestinations(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "fixed-peer", false: "wrong-peer"}[allowed], func(t *testing.T) {
			outer, peer := net.Pipe()
			defer peer.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			proxy, err := newTransportProxy(ctx, outer, Endpoint{IP: netip.MustParseAddr("8.8.8.8"), Port: 1194, Transport: "tcp4-client"})
			if err != nil {
				t.Fatal(err)
			}
			defer proxy.Close()
			conn, err := net.Dial("tcp4", proxy.listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(time.Second))
			conn.Write([]byte{5, 1, 0})
			var greeting [2]byte
			io.ReadFull(conn, greeting[:])
			if greeting != [2]byte{5, 0} {
				t.Fatal("greeting")
			}
			ip := byte(8)
			if !allowed {
				ip = 1
			}
			conn.Write([]byte{5, 1, 0, 1, ip, ip, ip, ip, 4, 170})
			var reply [10]byte
			_, err = io.ReadFull(conn, reply[:])
			if !allowed {
				if err == nil {
					t.Fatal("wrong peer accepted")
				}
				return
			}
			if err != nil || reply[1] != 0 {
				t.Fatal("fixed peer refused", err)
			}
			go peer.Write([]byte("tunnel"))
			var data [6]byte
			if _, err = io.ReadFull(conn, data[:]); err != nil || string(data[:]) != "tunnel" {
				t.Fatal(err)
			}
		})
	}
}
func TestFixedUDPTransportPreservesPackets(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	outer, err := net.DialUDP("udp4", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := newTransportProxy(t.Context(), outer, Endpoint{IP: netip.MustParseAddr("8.8.8.8"), Port: 1194, Transport: "udp4"})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	control, err := net.Dial("tcp4", proxy.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	control.SetDeadline(time.Now().Add(time.Second))
	control.Write([]byte{5, 1, 0})
	var methods [2]byte
	io.ReadFull(control, methods[:])
	control.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	var response [10]byte
	if _, err = io.ReadFull(control, response[:]); err != nil || response[1] != 0 {
		t.Fatal("UDP associate", err)
	}
	address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(binary.BigEndian.Uint16(response[8:]))}
	client, err := net.DialUDP("udp4", nil, address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	server.SetDeadline(time.Now().Add(time.Second))
	header := []byte{0, 0, 0, 1, 8, 8, 8, 8, 4, 170}
	client.Write(append(header, []byte("encrypted")...))
	var packet [100]byte
	n, from, err := server.ReadFromUDP(packet[:])
	if err != nil || string(packet[:n]) != "encrypted" {
		t.Fatal("datagram damaged", err)
	}
	server.WriteToUDP([]byte("reply"), from)
	n, err = client.Read(packet[:])
	if err != nil || string(packet[:10]) != string(header) || string(packet[10:n]) != "reply" {
		t.Fatal("SOCKS UDP response framing failed", err)
	}
}

func TestOpenVPNTCPNumericDomainForm(t *testing.T) {
	outer, peer := net.Pipe()
	defer peer.Close()
	proxy, err := newTransportProxy(t.Context(), outer, Endpoint{IP: netip.MustParseAddr("8.8.8.8"), Port: 1194, Transport: "tcp4-client"})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	conn, err := net.Dial("tcp4", proxy.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	conn.Write([]byte{5, 1, 0})
	var methods [2]byte
	io.ReadFull(conn, methods[:])
	// OpenVPN may encode even an already-pinned IP using SOCKS ATYP=domain.
	request := append([]byte{5, 1, 0, 3, 7}, []byte("8.8.8.8")...)
	request = append(request, 4, 170)
	conn.Write(request)
	var reply [10]byte
	if _, err = io.ReadFull(conn, reply[:]); err != nil || reply[1] != 0 {
		t.Fatal("exact numeric domain-form peer rejected", err)
	}
	go peer.Write([]byte("fixed-peer"))
	var data [10]byte
	if _, err = io.ReadFull(conn, data[:]); err != nil || string(data[:]) != "fixed-peer" {
		t.Fatal("lost transport data", err)
	}
}
