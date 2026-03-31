package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
)

// startEchoServer starts a TCP server that echoes received data back.
// Returns the port and a cleanup function.
func startEchoServer(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	t.Cleanup(func() { _ = lis.Close() })
	return port
}

// startCloseServer starts a TCP server that accepts and immediately closes.
func startCloseServer(t *testing.T) int {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	t.Cleanup(func() { _ = lis.Close() })
	return port
}

// tunnelPipe simulates a gRPC bidi stream for testing.
type tunnelPipe struct {
	mu       sync.Mutex
	incoming chan *operationspb.TunnelData // "client" → handler
	outgoing []*operationspb.TunnelData   // handler → "client"
	outCh    chan struct{}
}

func newTunnelPipe() *tunnelPipe {
	return &tunnelPipe{
		incoming: make(chan *operationspb.TunnelData, 100),
		outCh:    make(chan struct{}, 100),
	}
}

func (p *tunnelPipe) recv() (*operationspb.TunnelData, error) {
	msg, ok := <-p.incoming
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

func (p *tunnelPipe) send(msg *operationspb.TunnelData) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.outgoing = append(p.outgoing, msg)
	select {
	case p.outCh <- struct{}{}:
	default:
	}
	return nil
}

func (p *tunnelPipe) clientSend(msg *operationspb.TunnelData) {
	p.incoming <- msg
}

func (p *tunnelPipe) clientClose() {
	close(p.incoming)
}

func (p *tunnelPipe) waitOutput(n int, timeout time.Duration) []*operationspb.TunnelData {
	deadline := time.After(timeout)
	for {
		p.mu.Lock()
		if len(p.outgoing) >= n {
			out := make([]*operationspb.TunnelData, len(p.outgoing))
			copy(out, p.outgoing)
			p.mu.Unlock()
			return out
		}
		p.mu.Unlock()

		select {
		case <-p.outCh:
		case <-deadline:
			p.mu.Lock()
			out := make([]*operationspb.TunnelData, len(p.outgoing))
			copy(out, p.outgoing)
			p.mu.Unlock()
			return out
		}
	}
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestForward_EchoRoundTrip(t *testing.T) {
	port := startEchoServer(t)
	h := &ForwardHandler{}
	pipe := newTunnelPipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run handler in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.Handle(ctx, int32(port), "conn-1", pipe.recv, pipe.send)
	}()

	// Give handler time to connect.
	time.Sleep(100 * time.Millisecond)

	// Send data.
	pipe.clientSend(&operationspb.TunnelData{
		ConnectionId: "conn-1",
		Payload:      []byte("hello"),
	})

	// Wait for echo response.
	outputs := pipe.waitOutput(1, 3*time.Second)
	if len(outputs) == 0 {
		t.Fatal("no output received from echo server")
	}

	var echoed bytes.Buffer
	for _, o := range outputs {
		if !o.Close {
			echoed.Write(o.Payload)
		}
	}
	if echoed.String() != "hello" {
		t.Errorf("echo = %q, want %q", echoed.String(), "hello")
	}

	// Close client side.
	pipe.clientClose()

	select {
	case err := <-errCh:
		if err != nil {
			t.Logf("handler returned: %v (expected)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not exit after client close")
	}
}

func TestForward_PortUnreachable(t *testing.T) {
	h := &ForwardHandler{}
	pipe := newTunnelPipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use a port that is (very likely) not listening.
	err := h.Handle(ctx, 1, "conn-1", pipe.recv, pipe.send)
	if err == nil {
		t.Fatal("expected error for unreachable port")
	}

	// Should have sent a close signal.
	outputs := pipe.waitOutput(1, time.Second)
	if len(outputs) == 0 {
		t.Fatal("expected close signal for unreachable port")
	}
	if !outputs[0].Close {
		t.Error("expected close=true in response")
	}
}

func TestForward_ServerCloses(t *testing.T) {
	port := startCloseServer(t)
	h := &ForwardHandler{}
	pipe := newTunnelPipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- h.Handle(ctx, int32(port), "conn-1", pipe.recv, pipe.send)
	}()

	// Server immediately closes, handler should detect and send close.
	outputs := pipe.waitOutput(1, 3*time.Second)

	foundClose := false
	for _, o := range outputs {
		if o.Close {
			foundClose = true
		}
	}
	if !foundClose {
		t.Error("expected close signal when server closes connection")
	}

	pipe.clientClose()

	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not exit")
	}
}

func TestForward_ContextCancel(t *testing.T) {
	port := startEchoServer(t)
	h := &ForwardHandler{}
	pipe := newTunnelPipe()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- h.Handle(ctx, int32(port), "conn-1", pipe.recv, pipe.send)
	}()

	// Give handler time to connect.
	time.Sleep(100 * time.Millisecond)

	// Cancel context and close client channel (simulating gRPC stream close).
	cancel()
	pipe.clientClose()

	select {
	case <-errCh:
		// OK, handler exited.
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not exit after context cancel")
	}
}

func TestForward_HandleStream(t *testing.T) {
	port := startEchoServer(t)
	h := &ForwardHandler{}
	pipe := newTunnelPipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first := &operationspb.TunnelData{
		ConnectionId: "stream-1",
		Open: &operationspb.TunnelOpen{
			NodeId:     "node-1",
			RemotePort: int32(port),
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- h.HandleStream(ctx, first, pipe.recv, pipe.send)
	}()

	time.Sleep(100 * time.Millisecond)

	pipe.clientSend(&operationspb.TunnelData{
		ConnectionId: "stream-1",
		Payload:      []byte("via-stream"),
	})

	outputs := pipe.waitOutput(1, 3*time.Second)
	if len(outputs) == 0 {
		t.Fatal("no output received")
	}

	var echoed bytes.Buffer
	for _, o := range outputs {
		if !o.Close {
			echoed.Write(o.Payload)
		}
	}
	if echoed.String() != "via-stream" {
		t.Errorf("echo = %q, want %q", echoed.String(), "via-stream")
	}

	pipe.clientClose()

	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not exit")
	}
}

func TestForward_HandleStream_NoTunnelOpen(t *testing.T) {
	h := &ForwardHandler{}
	first := &operationspb.TunnelData{
		ConnectionId: "bad",
		Payload:      []byte("data"),
	}

	err := h.HandleStream(context.Background(), first, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing TunnelOpen")
	}
}

func TestForward_MultipleMessages(t *testing.T) {
	port := startEchoServer(t)
	h := &ForwardHandler{}
	pipe := newTunnelPipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- h.Handle(ctx, int32(port), "conn-1", pipe.recv, pipe.send)
	}()

	time.Sleep(100 * time.Millisecond)

	// Send multiple messages.
	for i := 0; i < 5; i++ {
		pipe.clientSend(&operationspb.TunnelData{
			ConnectionId: "conn-1",
			Payload:      []byte(fmt.Sprintf("msg%d-", i)),
		})
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for echoed data.
	outputs := pipe.waitOutput(3, 3*time.Second)

	var echoed bytes.Buffer
	for _, o := range outputs {
		if !o.Close {
			echoed.Write(o.Payload)
		}
	}

	expected := "msg0-msg1-msg2-msg3-msg4-"
	if echoed.String() != expected {
		t.Errorf("echo = %q, want %q", echoed.String(), expected)
	}

	pipe.clientClose()

	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not exit")
	}
}
