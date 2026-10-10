//go:build linux

package vpnipc

import (
	"context"
	"mikugate/internal/executoripc"
	"mikugate/internal/security"
	"mikugate/internal/streamio"
	"mikugate/internal/tunnel"
	"mikugate/internal/wirejson"
	"net"
	"os"
	"sync"
	"time"
)

type serverSession struct {
	engine *tunnel.Session
	cancel context.CancelFunc
	done   chan struct{}
}
type server struct {
	mu       sync.Mutex
	sessions map[string]*serverSession
	exits    map[string]bool
	uid      uint32
	options  tunnel.SessionOptions
	instance string
}

func Serve(ctx context.Context, instance string, uid uint32, options tunnel.SessionOptions, ready func(string)) error {
	if os.Geteuid() != 0 || options.Validate() != nil || options.WorkerUID == uid || options.TunnelUID == uid {
		return tunnel.ErrPrivilege
	}
	if err := tunnel.CheckSessionEnvironment(options); err != nil {
		return err
	}
	lease, err := executoripc.OpenVPNLease(instance, uid)
	if err != nil {
		return err
	}
	defer lease.Close()
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(life, func() { lease.Listener().Close() })
	defer stop()
	s := &server{sessions: map[string]*serverSession{}, exits: map[string]bool{}, uid: uid, options: options, instance: instance}
	if ready != nil {
		path, _ := executoripc.VPNSocketPath(instance)
		ready(path)
	}
	slots := make(chan struct{}, 100)
	var work sync.WaitGroup
	defer work.Wait()
	for {
		conn, err := lease.Listener().AcceptUnix()
		if err != nil {
			if life.Err() != nil {
				return nil
			}
			cancel()
			return err
		}
		if !executoripc.VPNPeerAllowed(conn, uid) {
			conn.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		work.Add(1)
		go func() { defer work.Done(); defer func() { <-slots }(); defer conn.Close(); s.handle(life, conn) }()
	}
}
func reply(conn net.Conn, value response) error {
	value.Version = Version
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	err := wirejson.Write(conn, value, MaxResponse)
	conn.SetWriteDeadline(time.Time{})
	return err
}
func (s *server) handle(parent context.Context, conn *net.UnixConn) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var req request
	if wirejson.Read(conn, &req, MaxRequest) != nil || !req.valid() {
		return
	}
	conn.SetReadDeadline(time.Time{})
	if req.Operation == "capabilities" {
		reply(conn, response{Capabilities: &Capabilities{Version: Version, Instance: s.instance, Mode: "isolated-openvpn", Options: s.options, MaxSessions: 3}})
		return
	}
	if req.Operation == "start" {
		s.mu.Lock()
		if s.exits[req.ExitID] || len(s.exits) >= 3 {
			s.mu.Unlock()
			reply(conn, response{Error: "busy"})
			return
		}
		s.exits[req.ExitID] = true
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.exits, req.ExitID); s.mu.Unlock() }()
		profile, err := tunnel.NormalizeWithOptions(req.Profile, req.IP, tunnel.NormalizeOptions{AllowVPNGateLegacy: req.Legacy, ExpectedPeerName: req.PeerName})
		req.Profile = ""
		if err != nil {
			reply(conn, response{Error: "profile"})
			return
		}
		// EOF from the owning control service cancels startup as well as established
		// sessions. Any extra data on the lifetime lease is a protocol violation.
		watcher := make(chan struct{})
		go func() { var b [1]byte; conn.Read(b[:]); cancel(); close(watcher) }()
		defer func() { conn.Close(); <-watcher }()
		engine, err := tunnel.StartSession(ctx, profile, s.options)
		if err != nil {
			reply(conn, response{Error: "unavailable"})
			return
		}
		token, err := security.Token(24)
		if err != nil {
			engine.Close()
			return
		}
		owned := &serverSession{engine: engine, cancel: cancel, done: make(chan struct{})}
		s.mu.Lock()
		s.sessions[token] = owned
		s.mu.Unlock()
		defer func() { engine.Close(); s.mu.Lock(); delete(s.sessions, token); s.mu.Unlock(); close(owned.done) }()
		info := engine.Info()
		if reply(conn, response{Token: token, Info: &info}) != nil {
			return
		}
		select {
		case <-engine.Done():
		case <-ctx.Done():
		}
		return
	}
	s.mu.Lock()
	owned := s.sessions[req.Token]
	s.mu.Unlock()
	if owned == nil {
		if req.Operation == "stop" {
			reply(conn, response{})
			return
		}
		reply(conn, response{Error: "unavailable"})
		return
	}
	switch req.Operation {
	case "stop":
		owned.cancel()
		select {
		case <-owned.done:
			reply(conn, response{})
		case <-ctx.Done():
		}
		return
	case "verify":
		bound, done := context.WithTimeout(ctx, 5*time.Second)
		err := owned.engine.Verify(bound)
		done()
		if err != nil {
			reply(conn, response{Error: "unavailable"})
			return
		}
		info := owned.engine.Info()
		reply(conn, response{Info: &info})
		return
	case "dial":
		bound, done := context.WithTimeout(ctx, 6*time.Second)
		up, err := owned.engine.DialContext(bound, "tcp4", req.Address)
		done()
		if err != nil {
			reply(conn, response{Error: "unavailable"})
			return
		}
		defer up.Close()
		if reply(conn, response{}) != nil {
			return
		}
		streamio.Bridge(ctx, conn, up)
	}
}
