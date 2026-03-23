// Package server provides the gRPC server and control plane implementation.
package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/auth"
)

// Router authenticates, authorizes, and resolves a nodeID to a registry.NodeEntry.
// It is shared by OperationsService and may be used by other services in the future.
type Router struct {
	reg *registry.Registry
}

var _ interface{ Route(context.Context, string) (*registry.NodeEntry, error) } = (*Router)(nil)

func newRouter(reg *registry.Registry) *Router {
	return &Router{reg: reg}
}

// Route validates the caller's claims from ctx, checks they are authorized to
// access nodeID, looks up the node (by ID first, then by name), and verifies
// it is online. Returns a gRPC status error on any failure.
func (r *Router) Route(ctx context.Context, nodeID string) (*registry.NodeEntry, error) {
	claims, ok := auth.ClaimsFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "router: missing auth claims")
	}

	if !auth.HasNodeAccess(claims, nodeID) {
		return nil, status.Errorf(codes.PermissionDenied, "router: access to node %q denied", nodeID)
	}

	// Try lookup by ID first, then fall back to name.
	entry, ok := r.reg.Lookup(nodeID)
	if !ok {
		entry, ok = r.reg.LookupByName(nodeID)
		if !ok {
			return nil, status.Errorf(codes.NotFound, "router: node %q not found", nodeID)
		}
	}

	if entry.Status != registry.StatusOnline {
		return nil, status.Errorf(codes.Unavailable, "router: node %q is offline", nodeID)
	}

	return entry, nil
}
