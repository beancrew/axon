package server

import (
	"net"

	"google.golang.org/grpc/test/bufconn"
)

// bufListener wraps a bufconn.Listener and exposes a Dial method that returns
// an in-memory net.Conn. This avoids any real network usage in tests.
type bufListener struct {
	*bufconn.Listener
}

func newBufListener(size int) *bufListener {
	return &bufListener{bufconn.Listen(size)}
}

// Dial returns a new in-memory connection to the listener.
func (b *bufListener) Dial() (net.Conn, error) {
	return b.Listener.Dial()
}
