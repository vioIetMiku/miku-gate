//go:build !linux

package tunnel

import (
	"context"
	"mikugate/internal/domain"
	"net"
)

type Session struct{}

func StartSession(context.Context, Profile, SessionOptions) (*Session, error) {
	return nil, ErrUnsupported
}
func (s *Session) Info() SessionInfo     { return SessionInfo{} }
func (s *Session) Done() <-chan struct{} { ch := make(chan struct{}); close(ch); return ch }
func (s *Session) Close() error          { return nil }
func (s *Session) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, ErrUnsupported
}
func (s *Session) Verify(context.Context) error { return ErrUnsupported }
func RunSupervisor() error                      { return ErrUnsupported }
func RunWorker() error                          { return ErrUnsupported }

func RunOpenVPN() error { return ErrUnsupported }

func CheckSessionEnvironment(SessionOptions) error { return ErrUnsupported }

func StartSessionThroughProxy(ctx context.Context, profile Profile, options SessionOptions, front domain.UpstreamConfig) (*Session, error) {
	return nil, ErrUnsupported
}
