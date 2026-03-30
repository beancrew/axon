package server

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/beancrew/axon/internal/server/registry"
	"github.com/beancrew/axon/pkg/auth"
)

// claimsCtx injects Claims into a context for testing.
func claimsCtx(claims *auth.Claims) context.Context {
	return auth.InjectClaims(context.Background(), claims)
}

func TestRouter_Route_NoClaims(t *testing.T) {
	reg := registry.NewRegistry(30 * time.Second)
	r := newRouter(reg)

	_, err := r.Route(context.Background(), "some-node")
	assertCode(t, err, codes.Unauthenticated)
}

func TestRouter_Route_PermissionDenied(t *testing.T) {
	reg := registry.NewRegistry(30 * time.Second)
	r := newRouter(reg)

	claims := &auth.Claims{UserID: "user1", NodeIDs: []string{"other-node"}, Kind: auth.KindCLI}
	ctx := claimsCtx(claims)

	_, err := r.Route(ctx, "my-node")
	assertCode(t, err, codes.PermissionDenied)
}

func TestRouter_Route_WildcardAccess(t *testing.T) {
	reg := registry.NewRegistry(30 * time.Second)
	_ = reg.Register("node-1", "web-1", "", registry.NodeInfo{})
	r := newRouter(reg)

	claims := &auth.Claims{UserID: "admin", NodeIDs: []string{"*"}, Kind: auth.KindCLI}
	ctx := claimsCtx(claims)

	entry, err := r.Route(ctx, "node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.NodeID != "node-1" {
		t.Errorf("got NodeID=%q, want %q", entry.NodeID, "node-1")
	}
}

func TestRouter_Route_NotFound(t *testing.T) {
	reg := registry.NewRegistry(30 * time.Second)
	r := newRouter(reg)

	claims := &auth.Claims{UserID: "user1", NodeIDs: []string{"*"}, Kind: auth.KindCLI}
	ctx := claimsCtx(claims)

	_, err := r.Route(ctx, "nonexistent")
	assertCode(t, err, codes.NotFound)
}

func TestRouter_Route_LookupByName(t *testing.T) {
	reg := registry.NewRegistry(30 * time.Second)
	_ = reg.Register("uuid-1", "web-1", "", registry.NodeInfo{})
	r := newRouter(reg)

	claims := &auth.Claims{UserID: "user1", NodeIDs: []string{"web-1"}, Kind: auth.KindCLI}
	ctx := claimsCtx(claims)

	entry, err := r.Route(ctx, "web-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.NodeID != "uuid-1" {
		t.Errorf("got NodeID=%q, want %q", entry.NodeID, "uuid-1")
	}
}

func TestRouter_Route_Offline(t *testing.T) {
	reg := registry.NewRegistry(30 * time.Second)
	_ = reg.Register("node-1", "web-1", "", registry.NodeInfo{})
	_ = reg.MarkOffline("node-1")
	r := newRouter(reg)

	claims := &auth.Claims{UserID: "user1", NodeIDs: []string{"*"}, Kind: auth.KindCLI}
	ctx := claimsCtx(claims)

	_, err := r.Route(ctx, "node-1")
	assertCode(t, err, codes.Unavailable)
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %v, got nil", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != want {
		t.Errorf("got code %v, want %v (msg: %s)", st.Code(), want, st.Message())
	}
}
