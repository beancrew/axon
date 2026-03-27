package agent

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	controlpb "github.com/garysng/axon/gen/proto/control"
	"github.com/garysng/axon/pkg/auth"
	"github.com/garysng/axon/pkg/config"
)

const (
	testSecret = "test-secret"
	bufSize    = 1024 * 1024
)

// mockControlService is a minimal ControlService server for testing.
type mockControlService struct {
	controlpb.UnimplementedControlServiceServer

	mu        sync.Mutex
	streams   []controlpb.ControlService_ConnectServer
	nodeNames []string

	heartbeatInterval int32
}

func newMockControlService(heartbeatInterval int32) *mockControlService {
	return &mockControlService{
		heartbeatInterval: heartbeatInterval,
	}
}

func (m *mockControlService) Connect(stream controlpb.ControlService_ConnectServer) error {
	// Expect RegisterRequest.
	msg, err := stream.Recv()
	if err != nil {
		return err
	}
	reg := msg.GetRegister()
	if reg == nil {
		return nil
	}

	// Validate token.
	if _, err := auth.ValidateToken(testSecret, reg.Token); err != nil {
		_ = stream.Send(&controlpb.ServerMessage{
			Payload: &controlpb.ServerMessage_RegisterResponse{
				RegisterResponse: &controlpb.RegisterResponse{
					Success: false,
					Error:   "invalid token",
				},
			},
		})
		return nil
	}

	nodeID := "test-node-id"

	m.mu.Lock()
	m.streams = append(m.streams, stream)
	m.nodeNames = append(m.nodeNames, reg.NodeName)
	m.mu.Unlock()

	// Send RegisterResponse.
	if err := stream.Send(&controlpb.ServerMessage{
		Payload: &controlpb.ServerMessage_RegisterResponse{
			RegisterResponse: &controlpb.RegisterResponse{
				Success:                  true,
				NodeId:                   nodeID,
				HeartbeatIntervalSeconds: m.heartbeatInterval,
			},
		},
	}); err != nil {
		return err
	}

	// Message loop: ack heartbeats, dispatch tasks.
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		switch msg.Payload.(type) {
		case *controlpb.AgentMessage_Heartbeat:
			if err := stream.Send(&controlpb.ServerMessage{
				Payload: &controlpb.ServerMessage_HeartbeatAck{
					HeartbeatAck: &controlpb.HeartbeatAck{
						ServerTimestamp: time.Now().UnixMilli(),
					},
				},
			}); err != nil {
				return err
			}
		}
	}
}

// sendTask sends a TaskSignal to the most recently connected agent.
func (m *mockControlService) sendTask(taskID string, taskType controlpb.TaskType) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.streams) == 0 {
		return nil
	}
	stream := m.streams[len(m.streams)-1]
	return stream.Send(&controlpb.ServerMessage{
		Payload: &controlpb.ServerMessage_TaskSignal{
			TaskSignal: &controlpb.TaskSignal{
				TaskId: taskID,
				Type:   taskType,
			},
		},
	})
}

// testEnv holds everything needed for a single test.
type testEnv struct {
	mock   *mockControlService
	lis    *bufconn.Listener
	server *grpc.Server
}

func newTestEnv(t *testing.T, heartbeatInterval int32) *testEnv {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	mock := newMockControlService(heartbeatInterval)
	controlpb.RegisterControlServiceServer(srv, mock)

	go func() { _ = srv.Serve(lis) }()

	t.Cleanup(func() {
		srv.Stop()
	})

	return &testEnv{mock: mock, lis: lis, server: srv}
}

func (e *testEnv) agentConfig(t *testing.T) config.AgentConfig {
	t.Helper()
	tok, _, err := auth.SignAgentToken(testSecret, "pre-node", time.Hour)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return config.AgentConfig{
		ServerAddr: "passthrough://bufnet",
		Token:      tok,
		NodeName:   "test-agent",
	}
}

// dialOption returns a gRPC dial option that uses the bufconn listener.
func (e *testEnv) dialOption() grpc.DialOption {
	return grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return e.lis.DialContext(ctx)
	})
}

// newTestAgent creates an Agent wired to the test env via bufconn.
func (e *testEnv) newTestAgent(t *testing.T) *Agent {
	t.Helper()
	cfg := e.agentConfig(t)
	a := NewAgent(cfg, "")
	// Override dial to use bufconn.
	a.dialOverride = e.dialOption()
	return a
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestAgent_RegisterAndHeartbeat(t *testing.T) {
	env := newTestEnv(t, 1) // 1 second heartbeat
	agent := env.newTestAgent(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run agent in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- agent.Run(ctx)
	}()

	// Wait for registration.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		env.mock.mu.Lock()
		n := len(env.mock.nodeNames)
		env.mock.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	env.mock.mu.Lock()
	if len(env.mock.nodeNames) == 0 {
		t.Fatal("agent did not register")
	}
	if env.mock.nodeNames[0] != "test-agent" {
		t.Errorf("node name = %q, want %q", env.mock.nodeNames[0], "test-agent")
	}
	env.mock.mu.Unlock()

	// Wait for at least one heartbeat cycle.
	time.Sleep(1500 * time.Millisecond)

	cancel()
	err := <-errCh
	if err != nil && err != context.Canceled {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgent_TaskDispatch(t *testing.T) {
	env := newTestEnv(t, 30) // long heartbeat to avoid noise
	agent := env.newTestAgent(t)

	var received sync.WaitGroup
	received.Add(1)

	var gotTaskID string
	var gotType controlpb.TaskType

	agent.SetTaskHandler(func(taskID string, taskType controlpb.TaskType) {
		gotTaskID = taskID
		gotType = taskType
		received.Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = agent.Run(ctx) }()

	// Wait for registration.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		env.mock.mu.Lock()
		n := len(env.mock.streams)
		env.mock.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Send a task.
	if err := env.mock.sendTask("task-42", controlpb.TaskType_TASK_EXEC); err != nil {
		t.Fatalf("sendTask: %v", err)
	}

	// Wait for handler.
	done := make(chan struct{})
	go func() {
		received.Wait()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for task handler")
	}

	if gotTaskID != "task-42" {
		t.Errorf("taskID = %q, want %q", gotTaskID, "task-42")
	}
	if gotType != controlpb.TaskType_TASK_EXEC {
		t.Errorf("taskType = %v, want %v", gotType, controlpb.TaskType_TASK_EXEC)
	}

	cancel()
}

func TestAgent_Reconnect(t *testing.T) {
	env := newTestEnv(t, 30)
	agent := env.newTestAgent(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() { _ = agent.Run(ctx) }()

	// Wait for first connection.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		env.mock.mu.Lock()
		n := len(env.mock.streams)
		env.mock.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	env.mock.mu.Lock()
	if len(env.mock.streams) == 0 {
		t.Fatal("agent did not connect")
	}
	env.mock.mu.Unlock()

	// Kill the server to force disconnect.
	env.server.Stop()

	// Restart a new server on the same listener.
	env.lis = bufconn.Listen(bufSize)
	env.server = grpc.NewServer()
	controlpb.RegisterControlServiceServer(env.server, env.mock)
	go func() { _ = env.server.Serve(env.lis) }()

	// Agent should reconnect. Wait for second registration.
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		env.mock.mu.Lock()
		n := len(env.mock.nodeNames)
		env.mock.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	env.mock.mu.Lock()
	count := len(env.mock.nodeNames)
	env.mock.mu.Unlock()

	if count < 2 {
		t.Errorf("expected at least 2 registrations (reconnect), got %d", count)
	}

	cancel()
	env.server.Stop()
}

func TestBackoff(t *testing.T) {
	// Attempt 1 should be ~2s.
	d1 := backoff(1)
	if d1 < 1*time.Second || d1 > 4*time.Second {
		t.Errorf("backoff(1) = %v, expected ~2s ±jitter", d1)
	}

	// High attempt should cap at ~60s.
	d100 := backoff(100)
	if d100 > 75*time.Second {
		t.Errorf("backoff(100) = %v, expected ≤ ~72s (60 + 20%% jitter)", d100)
	}
	if d100 < 45*time.Second {
		t.Errorf("backoff(100) = %v, expected ≥ ~48s (60 - 20%% jitter)", d100)
	}
}

func TestCollectNodeInfo(t *testing.T) {
	info := collectNodeInfo()
	if info.Hostname == "" {
		t.Error("expected non-empty hostname")
	}
	if info.Arch == "" {
		t.Error("expected non-empty arch")
	}
	if info.OsInfo == nil {
		t.Fatal("expected non-nil OsInfo")
	}
	if info.OsInfo.Os == "" {
		t.Error("expected non-empty OS")
	}
}
