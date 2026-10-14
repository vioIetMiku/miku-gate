package main

import (
	"bufio"
	"io"
	"net"
	"testing"
	"time"
)

// socksClientAuth 在 client 侧走一遍方法协商 + RFC1929 认证，
// 返回认证子协商的状态字节（0 成功、非 0 失败）。
func socksClientAuth(t *testing.T, c net.Conn, user, pass string) byte {
	t.Helper()
	// 只提供用户名口令认证方法
	if _, err := c.Write([]byte{socksVer5, 0x01, authUserPass}); err != nil {
		t.Fatalf("写方法协商失败: %v", err)
	}
	r := bufio.NewReader(c)
	sel := make([]byte, 2)
	if _, err := io.ReadFull(r, sel); err != nil {
		t.Fatalf("读方法选择失败: %v", err)
	}
	if sel[0] != socksVer5 || sel[1] != authUserPass {
		t.Fatalf("服务端未选择用户名口令认证: %v", sel)
	}
	msg := []byte{authSubVer, byte(len(user))}
	msg = append(msg, user...)
	msg = append(msg, byte(len(pass)))
	msg = append(msg, pass...)
	if _, err := c.Write(msg); err != nil {
		t.Fatalf("写认证失败: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(r, resp); err != nil {
		t.Fatalf("读认证结果失败: %v", err)
	}
	return resp[1]
}

func runServeSocks(cred *SocksCred) (net.Conn, func()) {
	sConn, cConn := net.Pipe()
	// dial 永不被调用（测试只到认证阶段），给个占位实现
	dial := func(network, addr string) (net.Conn, error) { return nil, io.EOF }
	go serveSocks(sConn, cred, dial)
	return cConn, func() { cConn.Close() }
}

func TestSocksAuthAcceptsCorrect(t *testing.T) {
	cred := &SocksCred{User: "alice", Pass: "s3cret"}
	c, done := runServeSocks(cred)
	defer done()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if code := socksClientAuth(t, c, "alice", "s3cret"); code != 0 {
		t.Fatalf("正确凭据应通过，得到状态 %d", code)
	}
}

func TestSocksAuthRejectsWrong(t *testing.T) {
	cred := &SocksCred{User: "alice", Pass: "s3cret"}
	c, done := runServeSocks(cred)
	defer done()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if code := socksClientAuth(t, c, "alice", "wrong"); code == 0 {
		t.Fatal("错误口令不应通过")
	}
}

// 客户端只报无认证时，要求认证的服务端必须拒绝，不能退回无认证。
func TestSocksAuthRejectsNoAuthOffer(t *testing.T) {
	cred := &SocksCred{User: "alice", Pass: "s3cret"}
	c, done := runServeSocks(cred)
	defer done()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := c.Write([]byte{socksVer5, 0x01, authNone}); err != nil {
		t.Fatalf("写方法协商失败: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil {
		t.Fatalf("读方法选择失败: %v", err)
	}
	if resp[1] != authNoAccept {
		t.Fatalf("应回 0xFF 拒绝，得到 %#x", resp[1])
	}
}

func TestValidateCred(t *testing.T) {
	ok := []SocksCred{
		{User: "fo123", Pass: "abcDEF234"},
		{User: "u", Pass: "p"},
	}
	for _, c := range ok {
		if err := validateCred(c); err != nil {
			t.Errorf("应通过 %+v: %v", c, err)
		}
	}
	bad := []SocksCred{
		{User: "", Pass: "x"},
		{User: "x", Pass: ""},
		{User: "a:b", Pass: "x"},
		{User: "a b", Pass: "x"},
		{User: "x", Pass: "a@b"},
		{User: "x", Pass: "a/b"},
	}
	for _, c := range bad {
		if err := validateCred(c); err == nil {
			t.Errorf("应拒绝 %+v", c)
		}
	}
}

func TestSocksURL(t *testing.T) {
	got := socksURL("1.2.3.4", 20000, SocksCred{User: "u", Pass: "p"})
	if got != "socks5://u:p@1.2.3.4:20000" {
		t.Fatalf("带凭据 URL 不对: %s", got)
	}
	got = socksURL("1.2.3.4", 20000, SocksCred{})
	if got != "socks5://1.2.3.4:20000" {
		t.Fatalf("无凭据 URL 不对: %s", got)
	}
}
