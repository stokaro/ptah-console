// Package freeze is a TCP proxy for tests that can stop carrying bytes while
// every connection stays open. From either end that is what a peer looks
// like when its process is frozen, or when the path to it drops packets
// without a word: the bytes are taken, and nothing comes back, not even a
// close.
package freeze

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Proxy carries connections to one address until Freeze.
type Proxy struct {
	listener net.Listener
	target   string
	frozen   atomic.Bool
	done     chan struct{}

	mu    sync.Mutex
	conns []net.Conn
}

// Start listens on a loopback port and carries what it accepts to target. It
// stops and closes every connection when the test ends.
func Start(t *testing.T, target string) *Proxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{listener: listener, target: target, done: make(chan struct{})}
	go p.accept()
	t.Cleanup(p.stop)
	return p
}

// Addr is the address to dial instead of the target.
func (p *Proxy) Addr() string {
	return p.listener.Addr().String()
}

// Freeze stops carrying bytes either way, and closes nothing.
func (p *Proxy) Freeze() {
	p.frozen.Store(true)
}

func (p *Proxy) accept() {
	for {
		in, err := p.listener.Accept()
		if err != nil {
			return
		}
		out, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = in.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, in, out)
		p.mu.Unlock()
		go p.carry(in, out)
		go p.carry(out, in)
	}
}

// carry copies from one end to the other. Frozen, it keeps reading, so the
// sender's writes succeed as they would into a frozen peer's socket buffer,
// and passes nothing on, not even the end of the stream.
func (p *Proxy) carry(from, to net.Conn) {
	buffer := make([]byte, 32<<10)
	for {
		n, err := from.Read(buffer)
		if !p.wait() {
			return
		}
		if n > 0 {
			if _, writeErr := to.Write(buffer[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if half, ok := to.(*net.TCPConn); ok {
					_ = half.CloseWrite()
				}
			}
			return
		}
	}
}

// wait blocks while the proxy is frozen, and reports false once it stops.
func (p *Proxy) wait() bool {
	for p.frozen.Load() {
		select {
		case <-p.done:
			return false
		case <-time.After(10 * time.Millisecond):
		}
	}
	return true
}

func (p *Proxy) stop() {
	close(p.done)
	_ = p.listener.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range p.conns {
		_ = conn.Close()
	}
}
