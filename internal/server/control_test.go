package server

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	controlpb "github.com/garysng/axon/gen/proto/control"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/auth"
)

const (
	testSecret    = "test-jwt-secret"
	testNodeName  = "test-node"
	bufSize       = 1024 * 1024
)

// testEnv holds all the pieces needed by a single test case.
type testEnv struct {
	server *Server
	client controlpb.ControlServiceClient
	cancel context.CancelFunc
}

// newTestEnv spins up a gRPC server backed by an in-process bufconn listener.
// The server stops when the returned cancel function is called.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	cfg := ServerConfig{
		JWTSecret:         testSecret,
		HeartbeatInterval: 30 * time.Second,
		HeartbeatTimeout:  5 * time.Minute,
	}

	srv := NewServer(cfg)

	// Build the internal components manually so we can use a custom listener.
	srv.registry = registry.NewRegistry(cfg.HeartbeatTimeout)
	srv.control = newControlService(srv.registry, cfg)

	opts, err := srv.buildServerOptions(nil)
	if err != nil {
		t.Fatalf("buildServerOptions: %v", err)
	}
	srv.grpc = grpc.NewServer(opts...)
	controlpb.RegisterControlServiceServer(srv.grpc, srv.control)

	// In-memory listener.
	lis := newBufListener(bufSize)

	ctx, cancel := context.WithCancel(context.Background())
	srv.registry.StartMonitor(ctx)

	go func() {
		_ = srv.grpc.Serve(lis)
	}()

	// Dial using the bufconn dialer.
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
		srv.grpc.Stop()
		_ = conn.Close()
	})

	return &testEnv{
		server: srv,
		client: controlpb.NewControlServiceClient(conn),
		cancel: cancel,
	}
}

// validAgentToken generates a short-lived agent token for testing.
func validAgentToken(t *testing.T) string {
	t.Helper()
	tok, _, err := auth.SignAgentToken(testSecret, "pre-node", time.Hour)
	if err != nil {
		t.Fatalf("SignAgentToken: %v", err)
	}
	return tok
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestConnect_RegisterSuccess(t *testing.T) {
	env := newTestEnv(t)

	stream, err := env.client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Send RegisterRequest.
	err = stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_Register{
			Register: &controlpb.RegisterRequest{
				Token:    validAgentToken(t),
				NodeName: testNodeName,
				Info: &controlpb.NodeInfo{
					Hostname: "host-1",
					Arch:     "amd64",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Send RegisterRequest: %v", err)
	}

	// Receive RegisterResponse.
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv RegisterResponse: %v", err)
	}

	rr := resp.GetRegisterResponse()
	if rr == nil {
		t.Fatal("expected RegisterResponse, got nil")
	}
	if !rr.Success {
		t.Fatalf("expected success=true, got error: %s", rr.Error)
	}
	if rr.NodeId == "" {
		t.Error("expected non-empty NodeId")
	}
	if rr.HeartbeatIntervalSeconds <= 0 {
		t.Error("expected positive HeartbeatIntervalSeconds")
	}

	// Verify node is in registry.
	nodeID := rr.NodeId
	entry, ok := env.server.Registry().Lookup(nodeID)
	if !ok {
		t.Fatalf("node %s not found in registry", nodeID)
	}
	if entry.Status != registry.StatusOnline {
		t.Errorf("status = %q, want %q", entry.Status, registry.StatusOnline)
	}
	if entry.NodeName != testNodeName {
		t.Errorf("NodeName = %q, want %q", entry.NodeName, testNodeName)
	}
}

func TestConnect_HeartbeatAck(t *testing.T) {
	env := newTestEnv(t)

	stream, err := env.client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Register first.
	if err := stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_Register{
			Register: &controlpb.RegisterRequest{
				Token:    validAgentToken(t),
				NodeName: "hb-node",
			},
		},
	}); err != nil {
		t.Fatalf("Send register: %v", err)
	}
	regResp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv register resp: %v", err)
	}
	if !regResp.GetRegisterResponse().Success {
		t.Fatal("registration failed")
	}

	// Send Heartbeat.
	before := time.Now().UnixMilli()
	if err := stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_Heartbeat{
			Heartbeat: &controlpb.Heartbeat{Timestamp: before},
		},
	}); err != nil {
		t.Fatalf("Send Heartbeat: %v", err)
	}

	// Receive HeartbeatAck.
	ack, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv HeartbeatAck: %v", err)
	}
	ha := ack.GetHeartbeatAck()
	if ha == nil {
		t.Fatal("expected HeartbeatAck, got nil")
	}
	if ha.ServerTimestamp < before {
		t.Errorf("server_timestamp %d < before %d", ha.ServerTimestamp, before)
	}
}

func TestConnect_InvalidToken(t *testing.T) {
	env := newTestEnv(t)

	stream, err := env.client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Send RegisterRequest with a bad token.
	if err := stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_Register{
			Register: &controlpb.RegisterRequest{
				Token:    "this.is.invalid",
				NodeName: "bad-node",
			},
		},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The server should either send a failure response or close the stream with
	// an Unauthenticated error.
	resp, err := stream.Recv()
	if err == nil {
		// Server may send a failure RegisterResponse before closing.
		rr := resp.GetRegisterResponse()
		if rr != nil && rr.Success {
			t.Fatal("expected registration to fail but got success=true")
		}
		// Next recv should return an error.
		_, err = stream.Recv()
	}
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
}

func TestConnect_NodeOfflineOnDisconnect(t *testing.T) {
	env := newTestEnv(t)

	stream, err := env.client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Register.
	if err := stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_Register{
			Register: &controlpb.RegisterRequest{
				Token:    validAgentToken(t),
				NodeName: "disc-node",
			},
		},
	}); err != nil {
		t.Fatalf("Send register: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv register: %v", err)
	}
	nodeID := resp.GetRegisterResponse().NodeId
	if nodeID == "" {
		t.Fatal("empty NodeId")
	}

	// Close the client side of the stream.
	_ = stream.CloseSend()

	// Give the server goroutine time to handle the EOF.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entry, ok := env.server.Registry().Lookup(nodeID)
		if ok && entry.Status == registry.StatusOffline {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	entry, _ := env.server.Registry().Lookup(nodeID)
	t.Errorf("node status = %q after disconnect, want %q", entry.Status, registry.StatusOffline)
}

func TestConnect_NodeInfoUpdate(t *testing.T) {
	env := newTestEnv(t)

	stream, err := env.client.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Register.
	if err := stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_Register{
			Register: &controlpb.RegisterRequest{
				Token:    validAgentToken(t),
				NodeName: "info-node",
				Info:     &controlpb.NodeInfo{Hostname: "old-host"},
			},
		},
	}); err != nil {
		t.Fatalf("Send register: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv register: %v", err)
	}
	nodeID := resp.GetRegisterResponse().NodeId

	// Send NodeInfo update.
	if err := stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_NodeInfo{
			NodeInfo: &controlpb.NodeInfo{
				Hostname: "new-host",
				Arch:     "arm64",
			},
		},
	}); err != nil {
		t.Fatalf("Send NodeInfo: %v", err)
	}

	// Wait for registry to reflect update.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entry, ok := env.server.Registry().Lookup(nodeID)
		if ok && entry.Info.Hostname == "new-host" {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	entry, _ := env.server.Registry().Lookup(nodeID)
	t.Errorf("Hostname = %q after NodeInfo update, want %q", entry.Info.Hostname, "new-host")
}
