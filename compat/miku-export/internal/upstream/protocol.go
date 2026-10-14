package upstream

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"

	"mikugate/internal/domain"
)

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *bufferedConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Close()
}
func send(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
func socksConnect(conn net.Conn, config domain.UpstreamConfig, host string, port uint16) (net.Conn, error) {
	method := byte(0)
	if config.Username != "" {
		method = 2
	}
	if err := send(conn, []byte{5, 1, method}); err != nil {
		return nil, ErrProtocol
	}
	r := bufio.NewReader(conn)
	var selected [2]byte
	if _, err := io.ReadFull(r, selected[:]); err != nil || selected[0] != 5 {
		return nil, ErrProtocol
	}
	// Never accept an anonymous downgrade after the operator supplied credentials.
	if selected[1] != method {
		return nil, ErrAuthentication
	}
	if method == 2 {
		auth := []byte{1, byte(len(config.Username))}
		auth = append(auth, []byte(config.Username)...)
		auth = append(auth, byte(len(config.Password)))
		auth = append(auth, []byte(config.Password)...)
		if err := send(conn, auth); err != nil {
			clear(auth)
			return nil, ErrAuthentication
		}
		clear(auth)
		if _, err := io.ReadFull(r, selected[:]); err != nil || selected[0] != 1 || selected[1] != 0 {
			return nil, ErrAuthentication
		}
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() {
		return nil, ErrDestination
	}
	request := []byte{5, 1, 0, 1}
	ip4 := ip.As4()
	request = append(request, ip4[:]...)
	request = binary.BigEndian.AppendUint16(request, port)
	if err := send(conn, request); err != nil {
		return nil, ErrProtocol
	}
	var header [4]byte
	if _, err = io.ReadFull(r, header[:]); err != nil || header[0] != 5 || header[1] != 0 || header[2] != 0 {
		return nil, ErrProtocol
	}
	size := 0
	switch header[3] {
	case 1:
		size = 4
	case 4:
		size = 16
	case 3:
		n, err := r.ReadByte()
		if err != nil || n == 0 {
			return nil, ErrProtocol
		}
		size = int(n)
	default:
		return nil, ErrProtocol
	}
	if _, err = io.CopyN(io.Discard, r, int64(size+2)); err != nil {
		return nil, ErrProtocol
	}
	return &bufferedConn{Conn: conn, reader: r}, nil
}
func httpConnect(conn net.Conn, config domain.UpstreamConfig, address string) (net.Conn, error) {
	header := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", address, address)
	if config.Username != "" {
		header += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(config.Username+":"+config.Password)) + "\r\n"
	}
	if err := send(conn, []byte(header+"\r\n")); err != nil {
		return nil, ErrProtocol
	}
	r := bufio.NewReader(conn)
	raw := make([]byte, 0, 1024)
	for len(raw) < 16*1024 {
		b, err := r.ReadByte()
		if err != nil {
			return nil, ErrProtocol
		}
		raw = append(raw, b)
		if bytes.HasSuffix(raw, []byte("\r\n\r\n")) {
			break
		}
	}
	if !bytes.HasSuffix(raw, []byte("\r\n\r\n")) {
		return nil, ErrProtocol
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), &http.Request{Method: http.MethodConnect})
	if err != nil {
		return nil, ErrProtocol
	}
	if response.StatusCode == 407 {
		return nil, ErrAuthentication
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || len(response.TransferEncoding) > 0 {
		return nil, ErrProtocol
	}
	// Response body bytes belong to the tunnel, including bytes buffered together
	// with the CONNECT headers. Do not drain or close an HTTP body here.
	return &bufferedConn{Conn: conn, reader: r}, nil
}
