package server

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"

	controlpb "github.com/garysng/axon/gen/proto/control"
	managementpb "github.com/garysng/axon/gen/proto/management"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/auth"
)

// fullTestEnv spins up a server with all services registered (control,
// operations, management) using an in-memory bufconn and audit store.
type fullTestEnv struct {
	server *Server
	conn   *grpc.ClientConn
	cancel context.CancelFunc
}

func newFullTestEnv(t *testing.T) *fullTestEnv {
	t.Helper()

	cfg := ServerConfig{
		JWTSecret:         testSecret,
		HeartbeatInterval: 30 * time.Second,
		HeartbeatTimeout:  5 * time.Minute,
		AuditDBPath:       ":memory:",
	}

	srv := NewServer(cfg)

	lis := newBufListener(bufSize)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = srv.serve(ctx, lis)
	}()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		cancel()
		t.Fatalf("grpc.NewClient: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		// Use Stop (not GracefulStop) because test agent streams may still be
		// open and GracefulStop would block waiting for them.
		if srv.grpc != nil {
			srv.grpc.Stop()
		}
		if srv.auditWriter != nil {
			_ = srv.auditWriter.Close()
		}
		_ = conn.Close()
	})

	return &fullTestEnv{server: srv, conn: conn, cancel: cancel}
}

// authedCtx adds a valid CLI JWT as gRPC authorization metadata.
func authedCtx(t *testing.T, userID string, nodeIDs []string) context.Context {
	t.Helper()
	tok, _, err := auth.SignCLIToken(testSecret, userID, nodeIDs, time.Hour)
	if err != nil {
		t.Fatalf("SignCLIToken: %v", err)
	}
	md := metadata.Pairs("authorization", "Bearer "+tok)
	return metadata.NewOutgoingContext(context.Background(), md)
}

// connectAgent registers a mock agent and returns the assigned nodeID.
func connectAgent(t *testing.T, env *fullTestEnv, nodeName string) string {
	t.Helper()

	cc := controlpb.NewControlServiceClient(env.conn)
	stream, err := cc.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	tok := validAgentToken(t)
	if err := stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_Register{
			Register: &controlpb.RegisterRequest{
				Token:    tok,
				NodeName: nodeName,
				Info: &controlpb.NodeInfo{
					Hostname: nodeName + "-host",
					Arch:     "amd64",
					Ip:       "10.0.0.1",
				},
			},
		},
	}); err != nil {
		t.Fatalf("Send register: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv register: %v", err)
	}
	rr := resp.GetRegisterResponse()
	if rr == nil || !rr.Success {
		t.Fatalf("registration failed: %v", rr)
	}

	// Keep stream alive in background (drain incoming messages).
	go func() {
		for {
			_, err := stream.Recv()
			if err != nil {
				return
			}
		}
	}()

	return rr.NodeId
}

// ── Management Tests ───────────────────────────────────────────────────────

func TestManagement_ListNodes_Empty(t *testing.T) {
	env := newFullTestEnv(t)
	mc := managementpb.NewManagementServiceClient(env.conn)

	ctx := authedCtx(t, "admin", []string{"*"})
	resp, err := mc.ListNodes(ctx, &managementpb.ListNodesRequest{})
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(resp.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(resp.Nodes))
	}
}

func TestManagement_ListNodes_WithAgent(t *testing.T) {
	env := newFullTestEnv(t)
	mc := managementpb.NewManagementServiceClient(env.conn)

	nodeID := connectAgent(t, env, "web-1")

	ctx := authedCtx(t, "admin", []string{"*"})
	resp, err := mc.ListNodes(ctx, &managementpb.ListNodesRequest{})
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(resp.Nodes))
	}
	if resp.Nodes[0].NodeId != nodeID {
		t.Errorf("NodeId = %q, want %q", resp.Nodes[0].NodeId, nodeID)
	}
	if resp.Nodes[0].NodeName != "web-1" {
		t.Errorf("NodeName = %q, want %q", resp.Nodes[0].NodeName, "web-1")
	}
	if resp.Nodes[0].Status != registry.StatusOnline {
		t.Errorf("Status = %q, want %q", resp.Nodes[0].Status, registry.StatusOnline)
	}
}

func TestManagement_GetNode(t *testing.T) {
	env := newFullTestEnv(t)
	mc := managementpb.NewManagementServiceClient(env.conn)

	nodeID := connectAgent(t, env, "db-1")

	ctx := authedCtx(t, "admin", []string{"*"})
	resp, err := mc.GetNode(ctx, &managementpb.GetNodeRequest{NodeId: nodeID})
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if resp.Summary == nil {
		t.Fatal("Summary is nil")
	}
	if resp.Summary.NodeId != nodeID {
		t.Errorf("NodeId = %q, want %q", resp.Summary.NodeId, nodeID)
	}
}

func TestManagement_GetNode_ByName(t *testing.T) {
	env := newFullTestEnv(t)
	mc := managementpb.NewManagementServiceClient(env.conn)

	connectAgent(t, env, "named-node")

	ctx := authedCtx(t, "admin", []string{"*"})
	resp, err := mc.GetNode(ctx, &managementpb.GetNodeRequest{NodeId: "named-node"})
	if err != nil {
		t.Fatalf("GetNode by name: %v", err)
	}
	if resp.Summary.NodeName != "named-node" {
		t.Errorf("NodeName = %q, want %q", resp.Summary.NodeName, "named-node")
	}
}

func TestManagement_GetNode_NotFound(t *testing.T) {
	env := newFullTestEnv(t)
	mc := managementpb.NewManagementServiceClient(env.conn)

	ctx := authedCtx(t, "admin", []string{"*"})
	_, err := mc.GetNode(ctx, &managementpb.GetNodeRequest{NodeId: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestManagement_RemoveNode(t *testing.T) {
	env := newFullTestEnv(t)
	mc := managementpb.NewManagementServiceClient(env.conn)

	nodeID := connectAgent(t, env, "rm-node")

	ctx := authedCtx(t, "admin", []string{"*"})
	resp, err := mc.RemoveNode(ctx, &managementpb.RemoveNodeRequest{NodeId: nodeID})
	if err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success=true, got error: %s", resp.Error)
	}

	// Verify node is gone.
	_, err = mc.GetNode(ctx, &managementpb.GetNodeRequest{NodeId: nodeID})
	if err == nil {
		t.Error("expected error after removal, got nil")
	}
}

func TestManagement_RemoveNode_NotFound(t *testing.T) {
	env := newFullTestEnv(t)
	mc := managementpb.NewManagementServiceClient(env.conn)

	ctx := authedCtx(t, "admin", []string{"*"})
	resp, err := mc.RemoveNode(ctx, &managementpb.RemoveNodeRequest{NodeId: "ghost"})
	if err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false for nonexistent node")
	}
}

func TestManagement_Login_Unimplemented(t *testing.T) {
	env := newFullTestEnv(t)
	mc := managementpb.NewManagementServiceClient(env.conn)

	_, err := mc.Login(context.Background(), &managementpb.LoginRequest{
		Username: "admin",
		Password: "pass",
	})
	if err == nil {
		t.Fatal("expected error from Login")
	}
	st, ok := grpcstatus.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.Unimplemented {
		t.Errorf("expected Unimplemented, got %v", st.Code())
	}
}
