//go:build linux

package tunnel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"mikugate/internal/fdwire"
	"mikugate/internal/streamio"
	"mikugate/internal/wirejson"
)

const sessionBootLimit = 256 << 10

type sessionBoot struct {
	Config    string         `json:"config"`
	IP        string         `json:"ip"`
	Port      uint16         `json:"port"`
	Transport string         `json:"transport"`
	Host      NamespaceID    `json:"host"`
	Options   SessionOptions `json:"options"`
}
type workerBoot struct {
	Host      NamespaceID `json:"host"`
	Expected  NamespaceID `json:"expected"`
	WorkerUID uint32      `json:"worker_uid"`
	Resolver  string      `json:"resolver"`
}
type sessionMessage struct {
	Operation string       `json:"operation"`
	Address   string       `json:"address,omitempty"`
	Info      *SessionInfo `json:"info,omitempty"`
	Error     string       `json:"error,omitempty"`
}
type Session struct {
	channel *fdwire.Channel
	process *exec.Cmd
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	info    SessionInfo
	once    sync.Once
}

func ownExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if err = trustedExecutable(path); err != nil {
		return "", err
	}
	return path, nil
}
func trustedExecutable(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrPrivilege
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		meta, ok := info.Sys().(*syscall.Stat_t)
		if !ok || meta.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return ErrPrivilege
		}
		if current == path && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || meta.Nlink != 1) {
			return ErrPrivilege
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}
func sealBoot(value any, uid uint32) (*os.File, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > sessionBootLimit {
		return nil, ErrProfile
	}
	fd, err := unix.MemfdCreate("miku-vpn-boot", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "sealed-boot")
	failed := true
	defer func() {
		if failed {
			file.Close()
		}
	}()
	if _, err = file.Write(raw); err != nil {
		return nil, err
	}
	if _, err = file.Seek(0, 0); err != nil {
		return nil, err
	}
	if err = file.Chown(int(uid), int(uid)); err != nil {
		return nil, err
	}
	if err = file.Chmod(0400); err != nil {
		return nil, err
	}
	if _, err = unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, err
	}
	failed = false
	return file, nil
}
func readBoot(fd uintptr, value any, uid uint32) error {
	file := os.NewFile(fd, "sealed-boot")
	if file == nil {
		return ErrPrivilege
	}
	defer file.Close()
	var info unix.Stat_t
	if unix.Fstat(int(fd), &info) != nil || info.Uid != uid || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Size <= 0 || info.Size > sessionBootLimit {
		return ErrPrivilege
	}
	seals, err := unix.FcntlInt(fd, unix.F_GET_SEALS, 0)
	if err != nil || seals&(unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) != (unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL) {
		return ErrPrivilege
	}
	raw, err := io.ReadAll(io.LimitReader(file, sessionBootLimit+1))
	if err != nil {
		return err
	}
	defer clear(raw)
	return wirejson.Decode(raw, value, sessionBootLimit)
}
func inheritedChannel(fd uintptr) (*fdwire.Channel, error) {
	file := os.NewFile(fd, "inherited-private-channel")
	if file == nil {
		return nil, ErrPrivilege
	}
	defer file.Close()
	peer, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Uid != 0 {
		return nil, ErrPrivilege
	}
	return fdwire.FromFile(file)
}

// StartSession accepts only a normalized Profile. Its fixed outer socket is
// opened before CLONE_NEWNET and passed only to the trusted root supervisor.
// OpenVPN never runs in the host namespace and never runs as root.
func StartSession(ctx context.Context, profile Profile, options SessionOptions) (*Session, error) {
	binary, err := ownExecutable()
	if err != nil {
		return nil, err
	}
	return startSession(ctx, profile, options, binary)
}
func startSession(ctx context.Context, profile Profile, options SessionOptions, binary string) (*Session, error) {
	if os.Geteuid() != 0 || options.Validate() != nil {
		return nil, ErrPrivilege
	}
	if profile.ca == "" || profile.peerName == "" || !profile.endpoint.IP.Is4() || profile.endpoint.Port == 0 {
		return nil, ErrProfile
	}
	if err := trustedExecutable(binary); err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	failed := true
	defer func() {
		if failed {
			cancel()
		}
	}()
	startup, stop := context.WithTimeout(life, 25*time.Second)
	defer stop()
	network := "tcp4"
	if profile.endpoint.Transport == "udp4" {
		network = "udp4"
	} else if profile.endpoint.Transport != "tcp4-client" {
		return nil, ErrProfile
	}
	outer, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(startup, network, net.JoinHostPort(profile.endpoint.IP.String(), strconv.Itoa(int(profile.endpoint.Port))))
	if err != nil {
		return nil, ErrSession
	}
	defer outer.Close()
	var outerFile *os.File
	switch c := outer.(type) {
	case *net.TCPConn:
		outerFile, err = c.File()
	case *net.UDPConn:
		outerFile, err = c.File()
	default:
		return nil, ErrSession
	}
	if err != nil {
		return nil, err
	}
	defer outerFile.Close()
	host, err := CurrentNamespace()
	if err != nil {
		return nil, err
	}
	boot, err := sealBoot(sessionBoot{Config: profile.render(), IP: profile.endpoint.IP.String(), Port: profile.endpoint.Port, Transport: profile.endpoint.Transport, Host: host, Options: options}, 0)
	if err != nil {
		return nil, err
	}
	defer boot.Close()
	parent, child, err := fdwire.Pair(true)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	defer child.Close()
	channel, err := fdwire.FromFile(parent)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(binary, "_vpn-supervisor")
	cmd.ExtraFiles = []*os.File{child, outerFile, boot}
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "GOMAXPROCS=1", "GOMEMLIMIT=80MiB"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNET, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = time.Second
	diagnostics := &processOutput{ready: make(chan struct{})}
	cmd.Stderr = diagnostics
	processDone, err := startParentBound(cmd)
	if err != nil {
		channel.Close()
		return nil, err
	}
	session := &Session{channel: channel, process: cmd, done: make(chan struct{}), ctx: life, cancel: cancel}
	go func() { <-processDone; cancel(); channel.Close(); close(session.done) }()
	after := context.AfterFunc(life, func() { channel.Close(); cmd.Process.Signal(syscall.SIGTERM) })
	defer func() {
		if failed {
			session.Close()
		}
		after()
	}()
	channel.Conn.SetReadDeadline(time.Now().Add(23 * time.Second))
	var ready sessionMessage
	fd, err := channel.Receive(&ready)
	if fd != nil {
		fd.Close()
		return nil, ErrSession
	}
	if err != nil || ready.Operation != "ready" || ready.Error != "" || ready.Info == nil {
		return nil, fmt.Errorf("%w: %s", ErrSession, diagnostics.text())
	}
	info := *ready.Info
	if info.Namespace == host || info.Namespace.Inode == 0 || info.InterfaceIndex <= 0 || info.TunnelUID != options.TunnelUID || info.WorkerUID != options.WorkerUID || len(info.GuardSHA256) != 64 {
		return nil, ErrSession
	}
	channel.Conn.SetReadDeadline(time.Time{})
	session.info = info
	// This watcher lasts for the session (not only startup).
	go func() { <-life.Done(); session.Close() }()
	failed = false
	return session, nil
}
func (s *Session) Info() SessionInfo     { return s.info }
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) Close() error {
	s.once.Do(func() {
		s.cancel()
		s.channel.Close()
		s.process.Process.Signal(syscall.SIGTERM)
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			s.process.Process.Kill()
			<-s.done
		}
	})
	return nil
}
func (s *Session) request(ctx context.Context, operation, address string) (net.Conn, error) {
	if s.ctx.Err() != nil {
		return nil, ErrSession
	}
	left, right, err := fdwire.Pair(false)
	if err != nil {
		return nil, err
	}
	defer left.Close()
	defer right.Close()
	stream, err := net.FileConn(left)
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	stop := context.AfterFunc(bounded, func() { stream.Close() })
	defer stop()
	if err = s.channel.Send(sessionMessage{Operation: operation, Address: address}, right); err != nil {
		stream.Close()
		return nil, ErrSession
	}
	deadline, _ := bounded.Deadline()
	stream.SetDeadline(deadline)
	var status [1]byte
	if _, err = io.ReadFull(stream, status[:]); err != nil || status[0] != 0 {
		stream.Close()
		if bounded.Err() != nil {
			return nil, bounded.Err()
		}
		return nil, ErrSession
	}
	if bounded.Err() != nil {
		stream.Close()
		return nil, bounded.Err()
	}
	stream.SetDeadline(time.Time{})
	return stream, nil
}
func (s *Session) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp4" {
		return nil, ErrDestination
	}
	return s.request(ctx, "dial", address)
}
func (s *Session) Verify(ctx context.Context) error {
	conn, err := s.request(ctx, "verify", "")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	var report SessionInfo
	if wirejson.Read(conn, &report, 2048) != nil || report != s.info {
		return ErrSession
	}
	return nil
}

func scopedCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	tool, err := trustedTool(name)
	if err != nil {
		return nil, err
	}
	bound, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bound, tool, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Dir = "/"
	cmd.WaitDelay = time.Second
	out := &processOutput{ready: make(chan struct{})}
	cmd.Stdout = out
	cmd.Stderr = out
	if cmd.Run() != nil {
		return nil, ErrSession
	}
	return []byte(out.text()), nil
}
func installSessionGuard(ctx context.Context, tunnelUID uint32) error {
	rules := "table inet miku_session {\n chain input { type filter hook input priority -300; policy drop; meta nfproto ipv6 drop; iifname lo accept; iifname " + Device + " accept; }\n chain output { type filter hook output priority -300; policy drop; meta nfproto ipv6 drop; oifname lo meta skuid { 0, " + strconv.FormatUint(uint64(tunnelUID), 10) + " } accept; oifname " + Device + " accept; }\n chain forward { type filter hook forward priority -300; policy drop; }\n}\n"
	tool, err := trustedTool("nft")
	if err != nil {
		return err
	}
	bound, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(bound, tool, "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	if err = cmd.Run(); err != nil {
		return ErrSession
	}
	return nil
}
func sessionGuardDigest(ctx context.Context) (string, error) {
	raw, err := scopedCommand(ctx, "nft", "-j", "list", "ruleset")
	if err != nil {
		return "", err
	}
	var parsed any
	if json.Unmarshal(raw, &parsed) != nil {
		return "", ErrSession
	}
	canonical, _ := json.Marshal(parsed)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func waitChild(cmd *exec.Cmd, done <-chan struct{}) (<-chan struct{}, func()) {
	var once sync.Once
	close := func() {
		once.Do(func() {
			cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				cmd.Process.Kill()
				<-done
			}
		})
	}
	return done, close
}
func spawnUnprivileged(binary string, args []string, files []*os.File, uid uint32, caps ...uintptr) (*exec.Cmd, error) {
	cmd := exec.Command(binary, args...)
	cmd.ExtraFiles = files
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "GOMAXPROCS=1", "GOMEMLIMIT=64MiB"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid, Groups: []uint32{}}, AmbientCaps: caps, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = time.Second
	return cmd, nil
}

// RunSupervisor is reachable only through the fixed internal exec entry point,
// as root, with sealed parent-owned configuration and inherited private sockets.
func RunSupervisor() error {
	if os.Geteuid() != 0 {
		return ErrPrivilege
	}
	parent, err := inheritedChannel(3)
	if err != nil {
		return err
	}
	defer parent.Close()
	var boot sessionBoot
	if readBoot(5, &boot, 0) != nil || boot.Options.Validate() != nil {
		return ErrPrivilege
	}
	current, err := CurrentNamespace()
	if err != nil || current == boot.Host || boot.Host.Inode == 0 {
		return ErrScope
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	stopParent := context.AfterFunc(ctx, func() { parent.Close() })
	defer stopParent()
	if _, err = scopedCommand(ctx, "ip", "link", "set", "lo", "up"); err != nil {
		return err
	}
	if err = installSessionGuard(ctx, boot.Options.TunnelUID); err != nil {
		return err
	}
	outerFile := os.NewFile(4, "fixed-vpn-transport")
	outer, err := net.FileConn(outerFile)
	outerFile.Close()
	if err != nil {
		return err
	}
	defer outer.Close()
	ip, err := netip.ParseAddr(boot.IP)
	if err != nil || !ip.Is4() {
		return ErrProfile
	}
	if outer.RemoteAddr().String() != net.JoinHostPort(boot.IP, strconv.Itoa(int(boot.Port))) {
		return ErrProfile
	}
	transport, err := newTransportProxy(ctx, outer, Endpoint{IP: ip, Port: boot.Port, Transport: boot.Transport})
	if err != nil {
		return err
	}
	defer transport.Close()
	// Canonical settings are generated by the trusted root parent, never copied
	// verbatim from the non-root API. The descriptor is sealed and private.
	config := boot.Config + "socks-proxy 127.0.0.1 " + strconv.Itoa(transport.Port()) + "\n"
	configFile, err := sealedConfig(config)
	if err != nil {
		return err
	}
	defer configFile.Close()
	if configFile.Chown(int(boot.Options.TunnelUID), int(boot.Options.TunnelUID)) != nil || configFile.Chmod(0400) != nil {
		return ErrPrivilege
	}
	binary, err := ownExecutable()
	if err != nil {
		return err
	}
	vpn, _ := spawnUnprivileged(binary, []string{"_vpn-openvpn"}, []*os.File{configFile}, boot.Options.TunnelUID, unix.CAP_NET_ADMIN)
	output := &processOutput{ready: make(chan struct{})}
	vpn.Stdout = output
	vpn.Stderr = output
	vpnWait, err := startParentBound(vpn)
	if err != nil {
		return err
	}
	vpnDone, closeVPN := waitChild(vpn, vpnWait)
	defer closeVPN()
	select {
	case <-output.ready:
	case <-vpnDone:
		return fmt.Errorf("%w: OpenVPN initialization: %s", ErrSession, output.text())
	case <-time.After(18 * time.Second):
		return ErrSession
	case <-ctx.Done():
		return ctx.Err()
	}
	if err = verifyChildIdentity(vpn, boot.Options.TunnelUID, 1<<unix.CAP_NET_ADMIN); err != nil {
		return err
	}
	if _, err = scopedCommand(ctx, "ip", "route", "replace", "default", "dev", Device); err != nil {
		return err
	}
	iface, err := net.InterfaceByName(Device)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return ErrScope
	}
	guard, err := sessionGuardDigest(ctx)
	if err != nil {
		return err
	}
	info := SessionInfo{Namespace: current, InterfaceIndex: iface.Index, TunnelUID: boot.Options.TunnelUID, WorkerUID: boot.Options.WorkerUID, GuardSHA256: guard}
	workerFile, childFile, err := fdwire.Pair(true)
	if err != nil {
		return err
	}
	defer workerFile.Close()
	defer childFile.Close()
	workerChannel, err := fdwire.FromFile(workerFile)
	if err != nil {
		return err
	}
	defer workerChannel.Close()
	workerConfig, err := sealBoot(workerBoot{Host: boot.Host, Expected: current, WorkerUID: boot.Options.WorkerUID, Resolver: "1.1.1.1:53"}, boot.Options.WorkerUID)
	if err != nil {
		return err
	}
	defer workerConfig.Close()
	worker, _ := spawnUnprivileged(binary, []string{"_vpn-worker"}, []*os.File{childFile, workerConfig}, boot.Options.WorkerUID, unix.CAP_NET_RAW)
	workerWait, err := startParentBound(worker)
	if err != nil {
		return err
	}
	workerDone, closeWorker := waitChild(worker, workerWait)
	defer closeWorker()
	workerChannel.Conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ready sessionMessage
	extra, e := workerChannel.Receive(&ready)
	if extra != nil {
		extra.Close()
		return ErrSession
	}
	if e != nil || ready.Operation != "ready" || ready.Error != "" {
		return ErrSession
	}
	workerChannel.Conn.SetReadDeadline(time.Time{})
	if err = verifyChildIdentity(worker, boot.Options.WorkerUID, 1<<unix.CAP_NET_RAW); err != nil {
		return err
	}
	if err = parent.Send(sessionMessage{Operation: "ready", Info: &info}, nil); err != nil {
		return err
	}
	go func() {
		select {
		case <-workerDone:
		case <-vpnDone:
		case <-transport.done:
		case <-ctx.Done():
		}
		cancel()
	}()
	for {
		var message sessionMessage
		file, err := parent.Receive(&message)
		if err != nil {
			return nil
		}
		if file == nil {
			return ErrSession
		}
		if message.Info != nil || message.Error != "" {
			file.Close()
			return ErrSession
		}
		switch message.Operation {
		case "dial":
			if len(message.Address) > 300 {
				file.Close()
				return ErrSession
			}
			err = workerChannel.Send(message, file)
			file.Close()
			if err != nil {
				return err
			}
		case "verify":
			conn, e := net.FileConn(file)
			file.Close()
			if e != nil {
				return ErrSession
			}
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			now, e := sessionGuardDigest(ctx)
			actual, aerr := net.InterfaceByName(Device)
			if message.Address != "" || e != nil || now != guard || aerr != nil || actual.Index != iface.Index || actual.Flags&net.FlagUp == 0 {
				conn.Write([]byte{1})
				conn.Close()
				return ErrScope
			}
			conn.Write([]byte{0})
			wirejson.Write(conn, info, 2048)
			conn.Close()
		default:
			file.Close()
			return ErrSession
		}
	}
}

func RunWorker() error {
	var boot workerBoot
	if readBoot(4, &boot, uint32(os.Geteuid())) != nil || os.Geteuid() == 0 || uint32(os.Geteuid()) != boot.WorkerUID {
		return ErrPrivilege
	}
	channel, err := inheritedChannel(3)
	if err != nil {
		return err
	}
	defer channel.Close()
	resolver, err := netip.ParseAddrPort(boot.Resolver)
	if err != nil || resolver.String() != "1.1.1.1:53" {
		return ErrScope
	}
	// Immutable no-new-privileges plus a non-root UID: the only ambient capability
	// is NET_RAW for device binding, not NET_ADMIN/SYS_ADMIN or filesystem powers.
	if !hasOnlyCapability(unix.CAP_NET_RAW) || unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
		return ErrPrivilege
	}
	dialer, err := NewWorkerDialer(DialOptions{Expected: boot.Expected, Host: boot.Host, Interface: Device, Resolver: resolver})
	if err != nil {
		return err
	}
	if err = channel.Send(sessionMessage{Operation: "ready"}, nil); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { channel.Close() })
	defer stop()
	slots := make(chan struct{}, 64)
	var work sync.WaitGroup
	defer func() { cancel(); work.Wait() }()
	for {
		var message sessionMessage
		file, err := channel.Receive(&message)
		if err != nil {
			return nil
		}
		if file == nil || message.Operation != "dial" || message.Info != nil || message.Error != "" || len(message.Address) > 300 {
			if file != nil {
				file.Close()
			}
			return ErrSession
		}
		conn, err := net.FileConn(file)
		file.Close()
		if err != nil {
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Write([]byte{1})
			conn.Close()
			continue
		}
		work.Add(1)
		go func(address string) {
			defer work.Done()
			defer func() { <-slots }()
			defer conn.Close()
			bounded, done := context.WithTimeout(ctx, 5*time.Second)
			upstream, err := dialer.DialContext(bounded, "tcp4", address)
			done()
			conn.SetWriteDeadline(time.Now().Add(time.Second))
			if err != nil {
				conn.Write([]byte{1})
				return
			}
			defer upstream.Close()
			if _, err = conn.Write([]byte{0}); err != nil {
				return
			}
			conn.SetWriteDeadline(time.Time{})
			streamio.Bridge(ctx, conn, upstream)
		}(message.Address)
	}
}

func hasOnlyCapability(capability uint32) bool {
	var header = unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if unix.Capget(&header, &data[0]) != nil {
		return false
	}
	return uint64(data[0].Effective)|(uint64(data[1].Effective)<<32) == uint64(1)<<capability
}
func verifyChildIdentity(cmd *exec.Cmd, uid uint32, capabilities uint64) error {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/status")
	if err != nil || len(raw) > 16384 {
		return ErrPrivilege
	}
	seenUID, seenGID, seenCaps, seenNNP := false, false, false, false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "Uid:", "Gid:":
			if len(fields) != 5 {
				return ErrPrivilege
			}
			for _, part := range fields[1:] {
				value, e := strconv.ParseUint(part, 10, 32)
				if e != nil || value != uint64(uid) {
					return ErrPrivilege
				}
			}
			if fields[0] == "Uid:" {
				seenUID = true
			} else {
				seenGID = true
			}
		case "CapEff:":
			value, e := strconv.ParseUint(fields[1], 16, 64)
			seenCaps = e == nil && value == capabilities
		case "NoNewPrivs:":
			seenNNP = fields[1] == "1"
		}
	}
	if !seenUID || !seenGID || !seenCaps || !seenNNP {
		return ErrPrivilege
	}
	return nil
}

// RunOpenVPN enters the official binary only after credentials/capabilities were
// restricted by the root parent. no_new_privs survives this exec and prevents
// inherited NET_ADMIN from being used to gain host filesystem privileges.
func RunOpenVPN() error {
	if os.Geteuid() == 0 || !hasOnlyCapability(unix.CAP_NET_ADMIN) || unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil {
		return ErrPrivilege
	}
	fd := 3
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size > MaxProfileBytes {
		return ErrPrivilege
	}
	seals, err := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	if err != nil || seals&unix.F_SEAL_WRITE == 0 {
		return ErrPrivilege
	}
	binary, err := trustedTool("openvpn")
	if err != nil {
		return err
	}
	return unix.Exec(binary, []string{binary, "--config", "/proc/self/fd/3"}, []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"})
}

// Linux parent-death signals track the creating thread, not merely the Go
// process. Keep that OS thread alive through Wait so runtime thread retirement
// cannot terminate a healthy OpenVPN/helper process.
func startParentBound(cmd *exec.Cmd) (<-chan struct{}, error) {
	started := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		err := cmd.Start()
		started <- err
		if err == nil {
			cmd.Wait()
		}
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return done, nil
}

// CheckSessionEnvironment is a read-only daemon preflight. It creates no socket,
// namespace or host rule, and refuses an untrusted executable before listening.
func CheckSessionEnvironment(options SessionOptions) error {
	if os.Geteuid() != 0 || options.Validate() != nil {
		return ErrPrivilege
	}
	if _, err := ownExecutable(); err != nil {
		return err
	}
	var header = unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var caps [2]unix.CapUserData
	if unix.Capget(&header, &caps[0]) != nil {
		return ErrPrivilege
	}
	effective := uint64(caps[0].Effective) | (uint64(caps[1].Effective) << 32)
	for _, capability := range []uint{unix.CAP_SYS_ADMIN, unix.CAP_NET_ADMIN, unix.CAP_NET_RAW, unix.CAP_SETUID, unix.CAP_SETGID, unix.CAP_SETPCAP} {
		if effective&(uint64(1)<<capability) == 0 {
			return ErrPrivilege
		}
	}
	info, err := os.Stat("/dev/net/tun")
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return ErrScope
	}
	for _, name := range []string{"openvpn", "ip", "nft"} {
		if _, err = trustedTool(name); err != nil {
			return err
		}
	}
	return nil
}
