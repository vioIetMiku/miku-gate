package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// SOCKS5 实现：支持 CONNECT（TCP）与 UDP ASSOCIATE（UDP 中继，见 udp.go）。
// 域名在本进程内解析，隧道里只跑 TCP 时不用隧道内的 UDP/DNS；
// UDP 中继则把客户端的 UDP 包放进隧道（netns）转发。
//
// 认证走 RFC1929 用户名/口令。端口对公网敞开，没有口令等于谁扫到谁就能用
// 这条家宽出口，所以凭据是必需的而不是可选项。

const (
	socksVer5       = 0x05
	authNone        = 0x00
	authUserPass    = 0x02
	authNoAccept    = 0xff
	authSubVer      = 0x01
	cmdConnect      = 0x01
	cmdUDPAssociate = 0x03
	atypIPv4        = 0x01
	atypDomain      = 0x03
	atypIPv6        = 0x04
	repSuccess      = 0x00
	repGenFail      = 0x01
	repHostUnre     = 0x04
	repCmdNotSupp   = 0x07
)

// serveSocks 处理一条 SOCKS5 连接。dial 决定流量从哪条链路出去。
// cred 为 nil 时不要求认证（内部调用路径不会走到，这里只是兜底）。
func serveSocks(client net.Conn, cred *SocksCred, dial func(network, addr string) (net.Conn, error)) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(30 * time.Second))

	if err := socksHandshake(client, cred); err != nil {
		return
	}

	cmd, addr, err := socksReadRequestWithCmd(client)
	if err != nil {
		if errors.Is(err, errCmdNotSupported) {
			socksReply(client, repCmdNotSupp)
		} else {
			socksReply(client, repGenFail)
		}
		return
	}

	if cmd == cmdUDPAssociate {
		_ = client.SetDeadline(time.Time{})
		serveUDPAssociate(client, dial)
		return
	}

	remote, err := dial("tcp", addr)
	if err != nil {
		socksReply(client, repHostUnre)
		return
	}
	defer remote.Close()

	if err := socksReply(client, repSuccess); err != nil {
		return
	}

	// 转发阶段不设整体超时，交给两端自然关闭
	_ = client.SetDeadline(time.Time{})
	_ = remote.SetDeadline(time.Time{})
	relay(client, remote)
}

// socksHandshake 完成方法协商，需要认证时接着跑一轮 RFC1929。
func socksHandshake(c net.Conn, cred *SocksCred) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil {
		return err
	}
	if head[0] != socksVer5 {
		return errors.New("不是 socks5")
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}

	if cred == nil || cred.User == "" {
		_, err := c.Write([]byte{socksVer5, authNone})
		return err
	}

	// 客户端没提用户名口令就直接拒绝，不退回无认证
	if !bytes.ContainsRune(methods, rune(authUserPass)) {
		_, _ = c.Write([]byte{socksVer5, authNoAccept})
		return errors.New("客户端不支持用户名口令认证")
	}
	if _, err := c.Write([]byte{socksVer5, authUserPass}); err != nil {
		return err
	}
	return socksAuth(c, cred)
}

// socksAuth 跑一轮 RFC1929 用户名/口令子协商。
func socksAuth(c net.Conn, cred *SocksCred) error {
	ver := make([]byte, 1)
	if _, err := io.ReadFull(c, ver); err != nil {
		return err
	}
	if ver[0] != authSubVer {
		return errors.New("认证子协议版本不对")
	}
	user, err := readLenPrefixed(c)
	if err != nil {
		return err
	}
	pass, err := readLenPrefixed(c)
	if err != nil {
		return err
	}

	// 恒定时间比较，避免按字节比对泄漏口令长度与前缀
	okUser := subtle.ConstantTimeCompare(user, []byte(cred.User)) == 1
	okPass := subtle.ConstantTimeCompare(pass, []byte(cred.Pass)) == 1
	if !okUser || !okPass {
		_, _ = c.Write([]byte{authSubVer, 0x01})
		return errors.New("用户名或口令不对")
	}
	_, err = c.Write([]byte{authSubVer, 0x00})
	return err
}

// readLenPrefixed 读一个单字节长度前缀的字段。
func readLenPrefixed(c net.Conn) ([]byte, error) {
	l := make([]byte, 1)
	if _, err := io.ReadFull(c, l); err != nil {
		return nil, err
	}
	b := make([]byte, int(l[0]))
	if _, err := io.ReadFull(c, b); err != nil {
		return nil, err
	}
	return b, nil
}

var errCmdNotSupported = errors.New("仅支持 CONNECT 与 UDP ASSOCIATE")

// errIPv6NotSupported 表示拒绝 IPv6 目标：隧道内只有 IPv4。
var errIPv6NotSupported = errors.New("隧道内不支持 IPv6")

// socksReadRequest 读请求并返回目标地址（仅 CONNECT，兼容旧调用）。
func socksReadRequest(c net.Conn) (string, error) {
	cmd, addr, err := socksReadRequestWithCmd(c)
	if err != nil {
		return "", err
	}
	if cmd != cmdConnect {
		return "", errCmdNotSupported
	}
	return addr, nil
}

// socksReadRequestWithCmd 读请求并返回命令与目标地址，支持 CONNECT 与 UDP ASSOCIATE。
func socksReadRequestWithCmd(c net.Conn) (byte, string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		return 0, "", err
	}
	switch head[1] {
	case cmdConnect, cmdUDPAssociate:
	default:
		return 0, "", errCmdNotSupported
	}

	var host string
	switch head[3] {
	case atypIPv4:
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return 0, "", err
		}
		host = net.IP(b).String()
	case atypIPv6:
		// 隧道内没有 IPv6 路由，放行只会让这条连接绕开隧道
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return 0, "", err
		}
		return 0, "", errIPv6NotSupported
	case atypDomain:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return 0, "", err
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(c, b); err != nil {
			return 0, "", err
		}
		host = string(b)
	default:
		return 0, "", fmt.Errorf("不支持的地址类型 %d", head[3])
	}

	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return 0, "", err
	}
	return head[1], net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb)))), nil
}

func socksReply(c net.Conn, code byte) error {
	_, err := c.Write([]byte{socksVer5, code, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
	return err
}

// socksReplyWith 回一个带 BND 地址/端口的应答（UDP ASSOCIATE 需要真实中继地址）。
func socksReplyWith(c net.Conn, code byte, ip net.IP, port int) error {
	if ip == nil || ip.To4() == nil {
		ip = net.IPv4zero
	}
	b := make([]byte, 0, 10)
	b = append(b, socksVer5, code, 0x00, atypIPv4)
	b = append(b, ip.To4()...)
	b = append(b, byte(port>>8), byte(port))
	_, err := c.Write(b)
	return err
}

func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}
