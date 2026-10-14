package executoripc

import (
	"context"
	"net"
	"time"
)

type service struct {
	instance  string
	runtime   Runtime
	operation chan struct{}
}

func newService(instance string, r Runtime) *service {
	return &service{instance: instance, runtime: r, operation: make(chan struct{}, 1)}
}
func (s *service) dispatch(ctx context.Context, r Request) response {
	out := response{Version: Version, ID: r.ID}
	if err := r.validate(); err != nil {
		out.Error = rpcError("invalid_request")
		return out
	}
	if err := ctx.Err(); err != nil {
		out.Error = classify(err)
		return out
	}
	if r.Operation == "capabilities" {
		caps := capabilities(s.instance)
		out.Capabilities = &caps
		return out
	}
	select {
	case s.operation <- struct{}{}:
		defer func() { <-s.operation }()
	default:
		out.Error = rpcError("busy")
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, OperationTimeout)
	defer cancel()
	var err error
	switch r.Operation {
	case "inspect":
		v, e := s.runtime.Inspect(ctx, r.ExitID)
		out.Result, err = &v, e
	case "prepare":
		v, e := s.runtime.Prepare(ctx, r.ExitID)
		out.Result, err = &v, e
	case "cleanup":
		v, e := s.runtime.Cleanup(ctx, r.ExitID)
		out.Result, err = &v, e
	}
	if err != nil {
		out.Result = nil
		out.Error = classify(err)
		return out
	}
	if !validResult(s.instance, r.ExitID, *out.Result) {
		out.Result = nil
		out.Error = rpcError("unverified")
	}
	return out
}
func (s *service) handle(parent context.Context, conn net.Conn) {
	defer conn.Close()
	stop := context.AfterFunc(parent, func() { conn.Close() })
	defer stop()
	if conn.SetReadDeadline(time.Now().Add(IOTimeout)) != nil {
		return
	}
	data, err := readFrame(conn, MaxRequestBytes)
	if err != nil {
		return
	}
	var request Request
	if decode(data, &request) != nil || request.validate() != nil {
		_ = conn.SetWriteDeadline(time.Now().Add(IOTimeout))
		_ = writeFrame(conn, response{Version: Version, ID: request.ID, Error: rpcError("invalid_request")}, MaxResponseBytes)
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		// Further bytes are forbidden. EOF/cancel means the caller disconnected.
		var byte [1]byte
		_, _ = conn.Read(byte[:])
		cancel()
	}()
	defer func() { conn.Close(); <-monitorDone }()
	out := s.dispatch(ctx, request)
	if ctx.Err() != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(IOTimeout))
	_ = writeFrame(conn, out, MaxResponseBytes)
}
