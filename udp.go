package main

import (
	"encoding/binary"
	"net"
	"strconv"
	"sync"
	"time"
)

// SOCKS5 UDP ASSOCIATE 支持。
// 原版 fanout 只实现 CONNECT，Xray 转发 hysteria2 客户端的 DNS/QUIC 等
// UDP 包时走 UDP ASSOCIATE 会被拒，导致手机端（DNS/QUIC 走隧道）连不上。
// 这里按 RFC1928 补上 UDP 中继：每个客户端 UDP 包在隧道(netns)内建立到
// 目标的已连接 UDP socket 转发，应答再按 SOCKS5 UDP 头回给客户端。

const udpIdleTimeout = 60 * time.Second

// udpSession 一条 (客户端, 目标) 对应的隧道内 UDP socket。
type udpSession struct {
	conn   net.Conn
	client *net.UDPAddr
	atyp   byte
	addr   []byte // 原始目标地址字节（回包头用）
	port   uint16
}

// serveUDPAssociate 处理 SOCKS5 UDP ASSOCIATE 请求。
// client 是已完成认证的 TCP 控制连接；dial 负责在隧道内建连。
func serveUDPAssociate(client net.Conn, dial func(network, addr string) (net.Conn, error)) {
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		_ = socksReply(client, repGenFail)
		return
	}
	defer relay.Close()

	// BND 地址：公网客户端连进来时用连接本端 IP，本机客户端就是 127.0.0.1
	bndIP := net.IPv4zero
	if la, ok := client.LocalAddr().(*net.TCPAddr); ok && la.IP != nil && !la.IP.IsUnspecified() {
		bndIP = la.IP
	}
	bndPort := relay.LocalAddr().(*net.UDPAddr).Port
	if err := socksReplyWith(client, repSuccess, bndIP, bndPort); err != nil {
		return
	}

	// TCP 控制连接关闭时结束整个 association
	go func() {
		_, _ = client.Read(make([]byte, 1))
		_ = relay.Close()
	}()

	var mu sync.Mutex
	sessions := make(map[string]*udpSession)
	defer func() {
		mu.Lock()
		for k, s := range sessions {
			_ = s.conn.Close()
			delete(sessions, k)
		}
		mu.Unlock()
	}()

	buf := make([]byte, 65535)
	for {
		_ = relay.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		n, src, err := relay.ReadFromUDP(buf)
		if err != nil {
			return
		}
		atyp, addrBytes, host, port, payload, ok := parseUDPReqHeader(buf[:n])
		if !ok {
			continue
		}
		dest := net.JoinHostPort(host, strconv.Itoa(int(port)))
		key := src.String() + "|" + dest

		mu.Lock()
		s := sessions[key]
		if s == nil {
			mu.Unlock()
			conn, derr := dial("udp", dest)
			if derr != nil {
				continue
			}
			mu.Lock()
			if s2 := sessions[key]; s2 != nil {
				_ = conn.Close()
				s = s2
			} else {
				s = &udpSession{conn: conn, client: src, atyp: atyp, addr: addrBytes, port: port}
				sessions[key] = s
				go relayUDP(s, relay, &mu, sessions, key)
			}
		}
		mu.Unlock()

		if _, werr := s.conn.Write(payload); werr != nil {
			// 隧道断了，reader 会负责清理
		}
	}
}

// relayUDP 读取隧道内 UDP socket 的应答，回写给客户端。
func relayUDP(s *udpSession, relay *net.UDPConn, mu *sync.Mutex, sessions map[string]*udpSession, key string) {
	buf := make([]byte, 65535)
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		n, err := s.conn.Read(buf)
		if err != nil {
			break
		}
		pkt := buildUDPRespHeader(s.atyp, s.addr, s.port, buf[:n])
		if _, err := relay.WriteToUDP(pkt, s.client); err != nil {
			break
		}
	}
	mu.Lock()
	_ = s.conn.Close()
	delete(sessions, key)
	mu.Unlock()
}

// parseUDPReqHeader 解析 SOCKS5 UDP 请求头（RSV FRAG ATYP ADDR PORT DATA）。
func parseUDPReqHeader(b []byte) (atyp byte, addrBytes []byte, host string, port uint16, payload []byte, ok bool) {
	if len(b) < 4 || b[0] != 0 || b[1] != 0 || b[2] != 0 {
		return 0, nil, "", 0, nil, false
	}
	atyp = b[3]
	pos := 4
	switch atyp {
	case atypIPv4:
		if len(b) < pos+4+2 {
			return 0, nil, "", 0, nil, false
		}
		addrBytes = append([]byte(nil), b[pos:pos+4]...)
		host = net.IP(b[pos : pos+4]).String()
		pos += 4
	case atypDomain:
		if len(b) < pos+1 {
			return 0, nil, "", 0, nil, false
		}
		l := int(b[pos])
		pos++
		if len(b) < pos+l+2 {
			return 0, nil, "", 0, nil, false
		}
		addrBytes = append([]byte(nil), b[pos:pos+l]...)
		host = string(b[pos : pos+l])
		pos += l
	case atypIPv6:
		return 0, nil, "", 0, nil, false // 隧道只支持 IPv4
	default:
		return 0, nil, "", 0, nil, false
	}
	if len(b) < pos+2 {
		return 0, nil, "", 0, nil, false
	}
	port = binary.BigEndian.Uint16(b[pos : pos+2])
	pos += 2
	return atyp, addrBytes, host, port, b[pos:], true
}

// buildUDPRespHeader 组装回给客户端的 SOCKS5 UDP 包头。
func buildUDPRespHeader(atyp byte, addrBytes []byte, port uint16, data []byte) []byte {
	head := make([]byte, 0, 4+len(addrBytes)+2)
	head = append(head, 0, 0, 0, atyp)
	head = append(head, addrBytes...)
	head = append(head, byte(port>>8), byte(port))
	return append(head, data...)
}
