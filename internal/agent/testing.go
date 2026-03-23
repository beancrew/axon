package agent

import "google.golang.org/grpc"

// SetDialOverride sets a gRPC dial option that replaces the network transport.
// This is intended for testing only (e.g. using bufconn).
func (a *Agent) SetDialOverride(opt grpc.DialOption) {
	a.dialOverride = opt
}
