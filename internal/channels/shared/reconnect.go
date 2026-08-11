package shared

import (
	"context"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	reconnectInitialDelay = 250 * time.Millisecond
	reconnectMaxDelay     = 15 * time.Second
)

// ReconnectDelay returns a bounded exponential retry delay with a small
// jitter so independently restarted channels do not reconnect in lockstep.
func ReconnectDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := reconnectInitialDelay
	for i := 0; i < attempt && delay < reconnectMaxDelay; i++ {
		delay *= 2
	}
	if delay > reconnectMaxDelay {
		delay = reconnectMaxDelay
	}
	// Keep the first retry responsive while spreading repeated retries by
	// +/- 20 percent.
	jitter := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(delay) * jitter)
}

// WaitForReconnect waits without leaving a timer behind when the channel is
// stopped while disconnected.
func WaitForReconnect(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		delay = reconnectInitialDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// IsPermanentConnectionError recognizes configuration/authentication errors
// that a reconnect loop cannot fix. Transport outages intentionally remain
// retryable.
func IsPermanentConnectionError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, " 401") ||
		strings.Contains(message, " 403") ||
		strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "forbidden") ||
		strings.Contains(message, "authentication failed") ||
		strings.Contains(message, "invalid token") ||
		strings.Contains(message, "invalid_auth") ||
		strings.Contains(message, "not_authed") ||
		strings.Contains(message, "token_revoked") ||
		strings.Contains(message, "token revoked") ||
		strings.Contains(message, "account_inactive")
}

// DialWebSocketContext dials with a copy of dialer that closes its underlying
// network connection when ctx is cancelled during the HTTP upgrade handshake.
// gorilla/websocket's DialContext uses the context for the TCP dial, but an
// already-open connection can otherwise remain blocked waiting for an upgrade
// response until HandshakeTimeout elapses. Call the returned finish function
// as soon as DialContext returns; a successful websocket connection remains
// owned by its caller.
func DialWebSocketContext(ctx context.Context, dialer *websocket.Dialer, url string, header http.Header) (*websocket.Conn, *http.Response, error) {
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	copyDialer := *dialer
	interrupter := newDialInterrupter(ctx)

	dialContext := dialer.NetDialContext
	if dialContext == nil {
		if dial := dialer.NetDial; dial != nil {
			dialContext = func(_ context.Context, network, address string) (net.Conn, error) {
				return dial(network, address)
			}
		} else {
			standard := &net.Dialer{}
			dialContext = standard.DialContext
		}
	}
	copyDialer.NetDialContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
		conn, err := dialContext(dialCtx, network, address)
		if err == nil {
			interrupter.capture(conn)
		}
		return conn, err
	}
	if dialTLSContext := dialer.NetDialTLSContext; dialTLSContext != nil {
		copyDialer.NetDialTLSContext = func(dialCtx context.Context, network, address string) (net.Conn, error) {
			conn, err := dialTLSContext(dialCtx, network, address)
			if err == nil {
				interrupter.capture(conn)
			}
			return conn, err
		}
	}

	defer interrupter.finish()
	return copyDialer.DialContext(ctx, url, header)
}

type dialInterrupter struct {
	mu       sync.Mutex
	conn     net.Conn
	canceled bool
	done     chan struct{}
	doneOnce sync.Once
}

func newDialInterrupter(ctx context.Context) *dialInterrupter {
	d := &dialInterrupter{done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			d.cancel()
		case <-d.done:
		}
	}()
	return d
}

func (d *dialInterrupter) capture(conn net.Conn) {
	d.mu.Lock()
	canceled := d.canceled
	if !canceled {
		d.conn = conn
	}
	d.mu.Unlock()
	if canceled {
		_ = conn.Close()
	}
}

func (d *dialInterrupter) cancel() {
	d.mu.Lock()
	if d.canceled {
		d.mu.Unlock()
		return
	}
	d.canceled = true
	conn := d.conn
	d.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (d *dialInterrupter) finish() {
	d.doneOnce.Do(func() { close(d.done) })
}
