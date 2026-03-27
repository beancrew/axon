package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	controlpb "github.com/garysng/axon/gen/proto/control"
	"github.com/garysng/axon/pkg/config"
)

// TaskHandler is called when the server dispatches a task to this agent.
// Implementations will open a data-plane stream to execute the task.
// Phase 1 (C-9): the default handler just logs the task.
type TaskHandler func(taskID string, taskType controlpb.TaskType)

// Agent manages the control-plane connection to the axon-server.
type Agent struct {
	cfg        config.AgentConfig
	configPath string

	nodeID   string // assigned by server after registration
	nodeName string

	taskHandler TaskHandler

	dataPlane bool // enable data plane dispatcher

	// dialOverride is an optional gRPC dial option used in tests to replace
	// the network transport (e.g. with bufconn).
	dialOverride grpc.DialOption

	mu     sync.Mutex
	conn   *grpc.ClientConn
	stream controlpb.ControlService_ConnectClient
}

// NewAgent creates a new Agent from the given config.
func NewAgent(cfg config.AgentConfig, configPath string) *Agent {
	name := cfg.NodeName
	if name == "" {
		if info := collectNodeInfo(); info.Hostname != "" {
			name = info.Hostname
		}
	}
	return &Agent{
		cfg:        cfg,
		configPath: configPath,
		nodeName:   name,
		taskHandler: func(taskID string, taskType controlpb.TaskType) {
			log.Printf("agent: received task %s (type=%v) — handler not implemented", taskID, taskType)
		},
	}
}

// SetTaskHandler registers a callback for incoming TaskSignal messages.
func (a *Agent) SetTaskHandler(h TaskHandler) {
	a.taskHandler = h
}

// EnableDataPlane enables the agent data plane dispatcher. When enabled,
// TaskSignals from the server will be handled by opening HandleTask streams.
func (a *Agent) EnableDataPlane() {
	a.dataPlane = true
}

// Run connects to the server and enters the main control loop. It reconnects
// with exponential backoff on connection failures. Run blocks until ctx is
// cancelled.
func (a *Agent) Run(ctx context.Context) error {
	var attempt int
	for {
		err := a.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		attempt++
		delay := backoff(attempt)
		log.Printf("agent: connection lost (%v), reconnecting in %s (attempt %d)", err, delay, attempt)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// runOnce establishes a single connection, registers, and runs the heartbeat +
// message loop until the connection drops or ctx is cancelled.
func (a *Agent) runOnce(ctx context.Context) error {
	conn, err := a.dial(ctx)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	a.mu.Lock()
	a.conn = conn
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.conn = nil
		a.stream = nil
		a.mu.Unlock()
	}()

	client := controlpb.NewControlServiceClient(conn)

	stream, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	a.mu.Lock()
	a.stream = stream
	a.mu.Unlock()

	// Register with the server.
	interval, err := a.register(stream)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}

	log.Printf("agent: registered as %q (id=%s), heartbeat every %s", a.nodeName, a.nodeID, interval)

	// Wire data plane dispatcher if enabled.
	if a.dataPlane {
		dispatcher := NewDispatcher(conn)
		a.SetTaskHandler(func(taskID string, taskType controlpb.TaskType) {
			dispatcher.HandleTask(ctx, taskID, taskType)
		})
		log.Printf("agent: data plane dispatcher enabled")
	}

	// Start heartbeat sender and message receiver.
	return a.controlLoop(ctx, stream, interval)
}

// dial creates a gRPC client connection to the configured server.
// Transport priority: ca_cert (strict TLS) > tls_insecure (TLS skip-verify) > plaintext.
func (a *Agent) dial(ctx context.Context) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{}

	switch {
	case a.cfg.CACert != "":
		creds, err := credentials.NewClientTLSFromFile(a.cfg.CACert, "")
		if err != nil {
			return nil, fmt.Errorf("load CA cert %q: %w", a.cfg.CACert, err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	case a.cfg.TLSInsecure:
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}))) //nolint:gosec
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	if a.dialOverride != nil {
		opts = append(opts, a.dialOverride)
	}

	return grpc.NewClient(a.cfg.ServerAddr, opts...)
}

// register sends the RegisterRequest and processes the response. It returns
// the heartbeat interval configured by the server.
func (a *Agent) register(stream controlpb.ControlService_ConnectClient) (time.Duration, error) {
	info := collectNodeInfo()

	if err := stream.Send(&controlpb.AgentMessage{
		Payload: &controlpb.AgentMessage_Register{
			Register: &controlpb.RegisterRequest{
				Token:    a.cfg.Token,
				NodeName: a.nodeName,
				Info:     info,
				NodeId:   a.cfg.NodeID,
			},
		},
	}); err != nil {
		return 0, fmt.Errorf("send register: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return 0, fmt.Errorf("recv register response: %w", err)
	}

	rr := resp.GetRegisterResponse()
	if rr == nil {
		return 0, fmt.Errorf("unexpected response type (expected RegisterResponse)")
	}
	if !rr.Success {
		return 0, fmt.Errorf("registration failed: %s", rr.Error)
	}

	a.nodeID = rr.NodeId

	// Persist node_id to config if this is the first registration.
	a.persistNodeID()

	interval := time.Duration(rr.HeartbeatIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return interval, nil
}

// controlLoop runs the concurrent heartbeat sender and message receiver.
// It exits when either goroutine encounters an error or ctx is cancelled.
func (a *Agent) controlLoop(ctx context.Context, stream controlpb.ControlService_ConnectClient, interval time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)

	// Heartbeat sender.
	go func() {
		errCh <- a.heartbeatLoop(ctx, stream, interval)
	}()

	// Message receiver (handles HeartbeatAck, TaskSignal, etc.).
	go func() {
		errCh <- a.recvLoop(ctx, stream)
	}()

	// Return on first error — the other goroutine will be cancelled.
	err := <-errCh
	cancel()
	return err
}

// heartbeatLoop sends heartbeats at the given interval until ctx is cancelled.
func (a *Agent) heartbeatLoop(ctx context.Context, stream controlpb.ControlService_ConnectClient, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := stream.Send(&controlpb.AgentMessage{
				Payload: &controlpb.AgentMessage_Heartbeat{
					Heartbeat: &controlpb.Heartbeat{
						Timestamp: time.Now().UnixMilli(),
					},
				},
			}); err != nil {
				return fmt.Errorf("send heartbeat: %w", err)
			}
		}
	}
}

// recvLoop reads incoming messages from the control stream and dispatches them.
func (a *Agent) recvLoop(ctx context.Context, stream controlpb.ControlService_ConnectClient) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("recv: %w", err)
		}

		switch p := msg.Payload.(type) {
		case *controlpb.ServerMessage_HeartbeatAck:
			// HeartbeatAck received — nothing to do.
			_ = p

		case *controlpb.ServerMessage_TaskSignal:
			if p.TaskSignal != nil && a.taskHandler != nil {
				go a.taskHandler(p.TaskSignal.TaskId, p.TaskSignal.Type)
			}

		default:
			log.Printf("agent: unknown message type: %T", p)
		}
	}
}

// persistNodeID writes the node_id back to the agent config file so it
// survives restarts. Errors are logged but not fatal.
func (a *Agent) persistNodeID() {
	if a.configPath == "" || a.nodeID == "" {
		return
	}
	// Re-read current config to preserve user edits, then update node_id.
	cfg, err := config.LoadAgentConfig(a.configPath)
	if err != nil {
		log.Printf("agent: persist node_id: load config: %v", err)
		return
	}
	cfg.NodeID = a.nodeID
	cfg.NodeName = a.nodeName
	if err := config.SaveAgentConfig(a.configPath, cfg); err != nil {
		log.Printf("agent: persist node_id: save config: %v", err)
	}
}

// backoff returns the delay before the next reconnection attempt.
// Exponential: min(2^attempt, 60) seconds with ±20% jitter.
func backoff(attempt int) time.Duration {
	base := math.Min(math.Pow(2, float64(attempt)), 60)
	jitter := base * 0.2 * (2*rand.Float64() - 1) // ±20%
	d := time.Duration((base + jitter) * float64(time.Second))
	if d < time.Second {
		d = time.Second
	}
	return d
}
