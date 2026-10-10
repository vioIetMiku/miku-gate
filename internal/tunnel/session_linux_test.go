//go:build linux

package tunnel

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"golang.org/x/net/dns/dnsmessage"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type sessionFixtureConfig struct {
	Profile string `json:"profile"`
}

func TestLinuxOpenVPNSessionWorkerLifecycle(t *testing.T) {
	for _, transport := range []string{"udp4", "tcp4-client"} {
		t.Run(transport, func(t *testing.T) {
			l := newVPNLab(t)
			before := l.hostSnapshot()
			l.installTopology()
			pki := testPKI(t)
			l.command(l.server, l.ip, "address", "add", "1.1.1.1/32", "dev", "mgservice0")
			l.command(l.server, l.ip, "address", "add", "93.184.216.34/32", "dev", "mgservice0")
			serverText := serverConfig(pki)
			if transport == "tcp4-client" {
				serverText = strings.Replace(serverText, "proto udp4", "proto tcp4-server", 1)
			}
			server, err := startProcess(l.ctx, l.server.file, serverText)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(server.close)
			l.ready(server)
			fixture := l.startFixture("session-services", l.server)
			t.Cleanup(fixture.close)
			fixture.wait("SERVICE_READY")
			raw := strings.Replace(pki.profile(transportServer), "proto udp4", "proto "+transport, 1)
			file, err := sealBoot(sessionFixtureConfig{Profile: raw}, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			child := exec.CommandContext(l.ctx, l.nsenter, "--net=/proc/self/fd/3", "--", l.binary, "-test.run=^TestSessionRootHarness$", "-test.v", "-test.timeout=60s")
			child.ExtraFiles = []*os.File{l.server.file, file}
			child.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "MIKU_SESSION_ROOT_HARNESS=1", "GOMAXPROCS=1", "GOMEMLIMIT=96MiB"}
			// CommandContext/Wait own this test coordinator; production children use startParentBound.
			child.WaitDelay = time.Second
			output, err := child.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "SESSION_PRODUCTION_PATH_OK") {
				t.Fatalf("real session helper failed: %v\n%s", err, output)
			}
			fixture.close()
			server.close()
			if string(before) != string(l.hostSnapshot()) {
				t.Fatal("production session lab altered host")
			}
			t.Logf("Observed: %s fixed-peer socket passed into anonymous namespace; actual OpenVPN UID65533 NET_ADMIN-only, worker UID65532 NET_RAW-only, no_new_privs; TUN-bound DNS/TCP via FD handoff; normal close and supervisor kill revoke old/new streams; host unchanged.", transport)
		})
	}
}
func TestSessionRootHarness(t *testing.T) {
	if os.Getenv("MIKU_SESSION_ROOT_HARNESS") != "1" {
		t.Skip("isolated parent harness only")
	}
	var config sessionFixtureConfig
	if err := readBoot(4, &config, 0); err != nil {
		t.Fatal(err)
	}
	profile, err := normalize(config.Profile, transportServer, time.Now(), func(ip string) error {
		if ip != transportServer {
			return ErrProfile
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, crash := range []bool{false, true} {
		s, err := startSession(t.Context(), profile, SessionOptions{TunnelUID: 65533, WorkerUID: 65532}, binary)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Verify(t.Context()); err != nil {
			s.Close()
			t.Fatal("namespace policy verification failed", err)
		}
		conn, err := s.DialContext(t.Context(), "tcp4", "session.test:18080")
		if err != nil {
			s.Close()
			t.Fatal("actual worker DNS/TCP failed", err)
		}
		text, err := echo(conn, "through-production-worker")
		if err != nil || !strings.HasPrefix(text, "FROM 10.243.0.") {
			conn.Close()
			s.Close()
			t.Fatal("source was not TUN", text, err)
		}
		if unexpected, e := s.DialContext(t.Context(), "tcp4", "127.0.0.1:80"); e == nil {
			unexpected.Close()
			t.Fatal("worker accepted loopback business target")
		}
		if crash {
			if err = s.process.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-s.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("supervisor kill not observed")
			}
		} else {
			s.Close()
		}
		if _, err = echo(conn, "must-not-survive"); err == nil {
			t.Fatal("old stream survived supervisor loss")
		}
		conn.Close()
		if fresh, err := s.DialContext(t.Context(), "tcp4", "session.test:18080"); err == nil {
			fresh.Close()
			t.Fatal("new stream survived session loss")
		}
		s.Close()
	}
	fmt.Println("SESSION_PRODUCTION_PATH_OK")
}
func runSessionServices(t *testing.T) {
	t.Helper()
	udp, err := net.ListenPacket("udp4", "1.1.1.1:53")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	tcp, err := net.Listen("tcp4", "93.184.216.34:18080")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	dnsTCP, err := net.Listen("tcp4", "1.1.1.1:53")
	if err != nil {
		t.Fatal(err)
	}
	defer dnsTCP.Close()
	answer := func(raw []byte) []byte {
		var query dnsmessage.Message
		if query.Unpack(raw) != nil || len(query.Questions) != 1 {
			return nil
		}
		response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true}, Questions: query.Questions, Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, Body: &dnsmessage.AResource{A: [4]byte{93, 184, 216, 34}}}}}
		packed, _ := response.Pack()
		return packed
	}
	go func() {
		var raw [4096]byte
		for {
			n, peer, err := udp.ReadFrom(raw[:])
			if err != nil {
				return
			}
			if data := answer(raw[:n]); data != nil {
				udp.WriteTo(data, peer)
			}
		}
	}()
	go func() {
		for {
			conn, err := dnsTCP.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(time.Second))
				var size [2]byte
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				n := int(binary.BigEndian.Uint16(size[:]))
				if n > 4096 {
					return
				}
				raw := make([]byte, n)
				if _, err := io.ReadFull(conn, raw); err != nil {
					return
				}
				data := answer(raw)
				binary.BigEndian.PutUint16(size[:], uint16(len(data)))
				conn.Write(append(size[:], data...))
			}()
		}
	}()
	go func() {
		for {
			conn, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
					fmt.Fprintln(conn, "FROM "+host+" "+scanner.Text())
				}
			}()
		}
	}()
	fmt.Println("SERVICE_READY")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
	}
}
