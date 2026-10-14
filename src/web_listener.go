package main

import (
	"bufio"
	"crypto/tls"
	"net"
	"sync"
	"time"
)

// Keep the TCP socket open while the management protocol changes. Each
// connection is classified concurrently so an idle client cannot block Accept.
type webProtocolListener struct {
	net.Listener
	tlsConfig *tls.Config
	ready     chan net.Conn
	done      chan struct{}
	slots     chan struct{}
	once      sync.Once
	mu        sync.Mutex
	pending   map[net.Conn]bool
	err       error
}

type bufferedWebConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedWebConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func newWebProtocolListener(base net.Listener, config *tls.Config) *webProtocolListener {
	l := &webProtocolListener{Listener: base, tlsConfig: config, ready: make(chan net.Conn), done: make(chan struct{}), slots: make(chan struct{}, 64), pending: map[net.Conn]bool{}}
	go l.receive()
	return l
}

func (l *webProtocolListener) receive() {
	var retryDelay time.Duration
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			if temporary, ok := err.(net.Error); ok && temporary.Temporary() {
				if retryDelay == 0 {
					retryDelay = 5 * time.Millisecond
				} else {
					retryDelay *= 2
				}
				if retryDelay > time.Second {
					retryDelay = time.Second
				}
				timer := time.NewTimer(retryDelay)
				select {
				case <-timer.C:
				case <-l.done:
					timer.Stop()
					return
				}
				continue
			}
			l.mu.Lock()
			l.err = err
			l.mu.Unlock()
			_ = l.Close()
			return
		}
		retryDelay = 0
		select {
		case l.slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		l.mu.Lock()
		select {
		case <-l.done:
			l.mu.Unlock()
			conn.Close()
			<-l.slots
			return
		default:
			l.pending[conn] = true
		}
		l.mu.Unlock()
		go l.classify(conn)
	}
}

func (l *webProtocolListener) classify(raw net.Conn) {
	defer func() { l.mu.Lock(); delete(l.pending, raw); l.mu.Unlock(); <-l.slots }()
	_ = raw.SetReadDeadline(time.Now().Add(8 * time.Second))
	reader := bufio.NewReaderSize(raw, 4096)
	prefix, err := reader.Peek(1)
	if err != nil {
		raw.Close()
		return
	}
	_ = raw.SetReadDeadline(time.Time{})
	var conn net.Conn = &bufferedWebConn{Conn: raw, reader: reader}
	if prefix[0] == 0x16 {
		conn = tls.Server(conn, l.tlsConfig)
	}
	select {
	case l.ready <- conn:
	case <-l.done:
		conn.Close()
	}
}

func (l *webProtocolListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.ready:
		return conn, nil
	case <-l.done:
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.err != nil {
			return nil, l.err
		}
		return nil, net.ErrClosed
	}
}

func (l *webProtocolListener) Close() error {
	var err error
	l.once.Do(func() {
		close(l.done)
		err = l.Listener.Close()
		l.mu.Lock()
		for conn := range l.pending {
			_ = conn.Close()
		}
		l.mu.Unlock()
	})
	return err
}
