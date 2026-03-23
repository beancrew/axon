package agent

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	operationspb "github.com/garysng/axon/gen/proto/operations"
)

const forwardBufSize = 32 * 1024 // 32 KB

// ForwardHandler relays traffic between a gRPC stream and a local TCP port.
type ForwardHandler struct{}

// Handle dials localhost:<remotePort> and relays data bidirectionally between
// the gRPC stream and the TCP connection. It blocks until either side closes
// or ctx is cancelled.
func (h *ForwardHandler) Handle(
	ctx context.Context,
	remotePort int32,
	connID string,
	recv func() (*operationspb.TunnelData, error),
	send func(*operationspb.TunnelData) error,
) error {
	addr := fmt.Sprintf("127.0.0.1:%d", remotePort)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		// Send close signal with error context, then return.
		_ = send(&operationspb.TunnelData{
			ConnectionId: connID,
			Close:        true,
		})
		return fmt.Errorf("forward: dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	// Cancel everything when ctx is done.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	// gRPC → TCP: read from stream, write to TCP.
	go func() {
		defer wg.Done()
		defer func() {
			if tc, ok := conn.(*net.TCPConn); ok {
				_ = tc.CloseWrite()
			}
		}()

		for {
			msg, err := recv()
			if err != nil {
				return
			}
			if msg.Close {
				return
			}
			if len(msg.Payload) > 0 {
				if _, err := conn.Write(msg.Payload); err != nil {
					return
				}
			}
		}
	}()

	// TCP → gRPC: read from TCP, write to stream.
	go func() {
		defer wg.Done()

		buf := make([]byte, forwardBufSize)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				if sendErr := send(&operationspb.TunnelData{
					ConnectionId: connID,
					Payload:      data,
				}); sendErr != nil {
					return
				}
			}
			if err != nil {
				// Send close signal on EOF or error.
				_ = send(&operationspb.TunnelData{
					ConnectionId: connID,
					Close:        true,
				})
				return
			}
		}
	}()

	wg.Wait()
	return nil
}

// HandleStream is a convenience wrapper that reads the first TunnelData
// message to extract TunnelOpen, then calls Handle with the remaining stream.
// This matches the server-side Forward RPC pattern.
func (h *ForwardHandler) HandleStream(
	ctx context.Context,
	first *operationspb.TunnelData,
	recv func() (*operationspb.TunnelData, error),
	send func(*operationspb.TunnelData) error,
) error {
	open := first.GetOpen()
	if open == nil {
		return fmt.Errorf("forward: first message must contain TunnelOpen")
	}

	connID := first.ConnectionId
	if connID == "" {
		connID = "default"
	}

	// If the first message also carries payload, we need to handle it.
	// Create a wrapper recv that returns the first payload then delegates.
	var firstPayloadConsumed bool
	wrappedRecv := func() (*operationspb.TunnelData, error) {
		if !firstPayloadConsumed {
			firstPayloadConsumed = true
			if len(first.Payload) > 0 {
				return &operationspb.TunnelData{
					ConnectionId: connID,
					Payload:      first.Payload,
				}, nil
			}
			if first.Close {
				return &operationspb.TunnelData{Close: true}, io.EOF
			}
		}
		return recv()
	}

	return h.Handle(ctx, open.RemotePort, connID, wrappedRecv, send)
}
