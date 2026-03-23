package server

import (
	"context"
	"net"
)

// ServeListener is a test-friendly wrapper around serve() that accepts an
// external net.Listener (e.g. bufconn). It initialises the server in the same
// way as Start but uses the provided listener instead of binding a TCP port.
func (s *Server) ServeListener(ctx context.Context, lis net.Listener) error {
	return s.serve(ctx, lis)
}
