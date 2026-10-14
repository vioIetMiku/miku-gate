package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mikugate/internal/datagram"
)

func udpTarget(raw []byte) (string, []byte, []byte, error) {
	if len(raw) < 7 || raw[0] != 0 || raw[1] != 0 || raw[2] != 0 {
		return "", nil, nil, errors.New("invalid or fragmented datagram")
	}
	offset := 4
	host := ""
	switch raw[3] {
	case 1:
		if len(raw) < 10 {
			return "", nil, nil, io.ErrUnexpectedEOF
		}
		host = net.IP(raw[4:8]).String()
		offset = 8
	case 3:
		size := int(raw[4])
		if size == 0 || len(raw) < 5+size+2 {
			return "", nil, nil, io.ErrUnexpectedEOF
		}
		host = string(raw[5 : 5+size])
		offset = 5 + size
	default:
		return "", nil, nil, errors.New("unsupported datagram address")
	}
	port := int(binary.BigEndian.Uint16(raw[offset : offset+2]))
	if port == 0 || len(host) > 253 || strings.ContainsAny(host, "\x00\r\n /\\:") || len(raw)-offset-2 > datagram.Maximum {
		return "", nil, nil, errors.New("invalid destination")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), raw[:offset+2], raw[offset+2:], nil
}

// Association lifetime follows its authenticated TCP connection. Only that
// connection's source IP and one pinned UDP source port may use the relay.
func (s *Server) udpAssociate(ctx context.Context, control net.Conn, reader *bufio.Reader, claimedHost string, claimedPort int) {
	reply := func(code byte) { _, _ = control.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0}) }
	if s.DialUDP == nil {
		reply(7)
		return
	}
	remote, ok := control.RemoteAddr().(*net.TCPAddr)
	local, lok := control.LocalAddr().(*net.TCPAddr)
	if !ok || !lok || remote.IP.To4() == nil || local.IP.To4() == nil || claimedHost != "0.0.0.0" && claimedHost != remote.IP.String() {
		reply(2)
		return
	}
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local.IP})
	if err != nil {
		reply(1)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	defer socket.Close()
	stop := context.AfterFunc(ctx, func() { socket.Close(); control.Close() })
	defer stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	control.SetDeadline(time.Time{})
	address := socket.LocalAddr().(*net.UDPAddr)
	response := []byte{5, 0, 0, 1}
	response = append(response, address.IP.To4()...)
	response = append(response, byte(address.Port>>8), byte(address.Port))
	if _, err = control.Write(response); err != nil {
		return
	}
	s.Metrics.udpStarted()
	defer s.Metrics.udpClosed()
	workers.Add(1)
	go func() { defer workers.Done(); var b [1]byte; reader.Read(b[:]); cancel() }()
	targets := map[string]net.Conn{}
	var targetsMu sync.Mutex
	defer func() {
		cancel()
		targetsMu.Lock()
		defer targetsMu.Unlock()
		for _, c := range targets {
			c.Close()
		}
	}()
	sourcePort := claimedPort
	buffer := make([]byte, 65536)
	var budget atomic.Int64
	window := time.Now()
	packets := 0
	for {
		socket.SetReadDeadline(time.Now().Add(45 * time.Second))
		n, source, err := socket.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		if !source.IP.Equal(remote.IP) || sourcePort != 0 && source.Port != sourcePort {
			continue
		}
		if time.Since(window) >= time.Second {
			window = time.Now()
			packets = 0
		}
		packets++
		if packets > 64 {
			continue
		}
		target, header, payload, err := udpTarget(buffer[:n])
		if err != nil {
			continue
		}
		if sourcePort == 0 {
			sourcePort = source.Port
		}
		if budget.Add(int64(len(payload))) > 32<<20 {
			return
		}
		targetsMu.Lock()
		up := targets[target]
		count := len(targets)
		targetsMu.Unlock()
		if up == nil {
			if count >= 8 {
				continue
			}
			bounded, done := context.WithTimeout(ctx, 6*time.Second)
			up, err = s.DialUDP(bounded, target)
			done()
			if err != nil || up == nil {
				s.Metrics.dialFailed()
				continue
			}
			targetsMu.Lock()
			targets[target] = up
			targetsMu.Unlock()
			prefix := append([]byte{}, header...)
			client := &net.UDPAddr{IP: append(net.IP{}, remote.IP...), Port: sourcePort}
			workers.Add(1)
			go func(conn net.Conn) {
				defer func() {
					conn.Close()
					targetsMu.Lock()
					if targets[target] == conn {
						delete(targets, target)
					}
					targetsMu.Unlock()
				}()
				defer workers.Done()
				incoming := make([]byte, datagram.Maximum+1)
				for {
					conn.SetReadDeadline(time.Now().Add(45 * time.Second))
					n, err := conn.Read(incoming)
					if err != nil || n > datagram.Maximum {
						return
					}
					if budget.Add(int64(n)) > 32<<20 {
						cancel()
						return
					}
					envelope := append(append([]byte{}, prefix...), incoming[:n]...)
					socket.SetWriteDeadline(time.Now().Add(2 * time.Second))
					if _, err = socket.WriteToUDP(envelope, client); err != nil {
						return
					}
					s.Metrics.udpDownload(n)
				}
			}(up)
		}
		up.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if written, err := up.Write(payload); err == nil && written == len(payload) {
			s.Metrics.udpUpload(written)
		}
	}
}
