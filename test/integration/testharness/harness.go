// Package testharness provides a reusable integration test harness that
// spins up an axon-server and axon-agent in-process using bufconn.
package testharness

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	controlpb "github.com/garysng/axon/gen/proto/control"
	"github.com/garysng/axon/internal/agent"
	"github.com/garysng/axon/internal/server"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/auth"
	"github.com/garysng/axon/pkg/config"
)

const bufSize = 1024 * 1024

// Harness holds a running server and agent connected via bufconn.
type Harness struct {
	t      *testing.T
	srv    *server.Server
	agt    *agent.Agent
	lis    *bufconn.Listener
	cancel context.CancelFunc

	opts harnessOpts

	agentToken  string // JWT token used by the primary agent
	agentNodeID string
	agentCancel context.CancelFunc
}

type harnessOpts struct {
	jwtSecret         string
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
	agentNodeName     string
	users             []server.UserEntry
}

func defaultOpts() harnessOpts {
	return harnessOpts{
		jwtSecret:         "integration-test-secret",
		heartbeatInterval: 30 * time.Second,
		heartbeatTimeout:  5 * time.Minute,
		agentNodeName:     "integration-agent",
	}
}

// HarnessOption configures the test harness.
type HarnessOption func(*harnessOpts)

// WithJWTSecret sets the JWT secret used by the server.
func WithJWTSecret(secret string) HarnessOption {
	return func(o *harnessOpts) { o.jwtSecret = secret }
}

// WithHeartbeatInterval sets the heartbeat interval advertised to agents.
func WithHeartbeatInterval(d time.Duration) HarnessOption {
	return func(o *harnessOpts) { o.heartbeatInterval = d }
}

// WithHeartbeatTimeout sets the heartbeat timeout for the registry monitor.
func WithHeartbeatTimeout(d time.Duration) HarnessOption {
	return func(o *harnessOpts) { o.heartbeatTimeout = d }
}

// WithAgentNodeName sets the node name used by the agent.
func WithAgentNodeName(name string) HarnessOption {
	return func(o *harnessOpts) { o.agentNodeName = name }
}

// WithUsers sets the CLI user credentials for Login tests.
func WithUsers(users []server.UserEntry) HarnessOption {
	return func(o *harnessOpts) { o.users = users }
}

// NewHarness creates a new integration test harness. It starts the server and
// connects an agent, blocking until the agent appears in the registry.
func NewHarness(t *testing.T, options ...HarnessOption) *Harness {
	t.Helper()

	opts := defaultOpts()
	for _, o := range options {
		o(&opts)
	}

	lis := bufconn.Listen(bufSize)

	srvCfg := server.ServerConfig{
		JWTSecret:         opts.jwtSecret,
		HeartbeatInterval: opts.heartbeatInterval,
		HeartbeatTimeout:  opts.heartbeatTimeout,
		Users:             opts.users,
	}
	srv := server.NewServer(srvCfg)

	ctx, cancel := context.WithCancel(context.Background())

	// Start server in background, waiting for initialisation to complete
	// before proceeding. This avoids a data race between serve() writing
	// s.registry and the test calling Registry().
	srvReady := make(chan struct{})
	srvErrCh := make(chan error, 1)
	go func() {
		srvErrCh <- srv.ServeListenerReady(ctx, lis, srvReady)
	}()
	<-srvReady // wait for server internals to be initialised
	_ = srvErrCh

	// Build agent config.
	agentToken, _, err := auth.SignAgentToken(opts.jwtSecret, "integration-node", time.Hour)
	if err != nil {
		cancel()
		t.Fatalf("harness: sign agent token: %v", err)
	}

	agentCfg := config.AgentConfig{
		ServerAddr:  "passthrough://bufnet",
		Token:       agentToken,
		NodeName:    opts.agentNodeName,
		TLSInsecure: true,
	}

	agt := agent.NewAgent(agentCfg, "")
	agt.SetDialOverride(grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}))

	// Start agent in background.
	agentCtx, agentCancel := context.WithCancel(ctx)
	go func() {
		_ = agt.Run(agentCtx)
	}()

	h := &Harness{
		t:           t,
		srv:         srv,
		agt:         agt,
		lis:         lis,
		cancel:      cancel,
		opts:        opts,
		agentToken:  agentToken,
		agentCancel: agentCancel,
	}

	// Wait for agent to appear in registry.
	h.agentNodeID = h.waitForAgent()

	t.Cleanup(h.Close)
	return h
}

// NewHarnessServerOnly creates a harness with only the server (no agent).
func NewHarnessServerOnly(t *testing.T, options ...HarnessOption) *Harness {
	t.Helper()

	opts := defaultOpts()
	for _, o := range options {
		o(&opts)
	}

	lis := bufconn.Listen(bufSize)

	srvCfg := server.ServerConfig{
		JWTSecret:         opts.jwtSecret,
		HeartbeatInterval: opts.heartbeatInterval,
		HeartbeatTimeout:  opts.heartbeatTimeout,
		Users:             opts.users,
	}
	srv := server.NewServer(srvCfg)

	ctx, cancel := context.WithCancel(context.Background())

	srvReady := make(chan struct{})
	srvErrCh := make(chan error, 1)
	go func() {
		srvErrCh <- srv.ServeListenerReady(ctx, lis, srvReady)
	}()
	<-srvReady // wait for server internals to be initialised
	_ = srvErrCh

	h := &Harness{
		t:      t,
		srv:    srv,
		lis:    lis,
		cancel: cancel,
		opts:   opts,
	}

	t.Cleanup(h.Close)
	return h
}

// Server returns the server instance.
func (h *Harness) Server() *server.Server { return h.srv }

// Registry returns the server's node registry.
func (h *Harness) Registry() *registry.Registry { return h.srv.Registry() }

// AgentNodeID returns the node ID assigned to the agent.
func (h *Harness) AgentNodeID() string { return h.agentNodeID }

// AgentToken returns the JWT token used by the primary agent.
func (h *Harness) AgentToken() string { return h.agentToken }

// CLIConn returns a gRPC client connection authenticated with a CLI JWT token
// that has wildcard ("*") node access.
func (h *Harness) CLIConn() *grpc.ClientConn {
	h.t.Helper()
	return h.CLIConnWithAccess("*")
}

// CLIConnWithAccess returns a gRPC client connection authenticated with a CLI
// JWT token that grants access to the specified node IDs.
func (h *Harness) CLIConnWithAccess(nodeIDs ...string) *grpc.ClientConn {
	h.t.Helper()

	token, _, err := auth.SignCLIToken(h.opts.jwtSecret, "test-user", nodeIDs, time.Hour)
	if err != nil {
		h.t.Fatalf("harness: sign CLI token: %v", err)
	}

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return h.lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(authUnaryInterceptor(token)),
		grpc.WithStreamInterceptor(authStreamInterceptor(token)),
	)
	if err != nil {
		h.t.Fatalf("harness: dial CLI conn: %v", err)
	}

	h.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// UnauthConn returns a gRPC client connection with no authentication.
// Useful for Login tests that don't require a pre-existing token.
func (h *Harness) UnauthConn() *grpc.ClientConn {
	h.t.Helper()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return h.lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		h.t.Fatalf("harness: dial unauth conn: %v", err)
	}

	h.t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// ConnectAgent connects a new agent instance and waits for it to register.
// Returns the agent and its node ID. The agent is stopped when the harness
// is closed.
func (h *Harness) ConnectAgent(name string) (*agent.Agent, string) {
	h.t.Helper()

	agentToken, _, err := auth.SignAgentToken(h.opts.jwtSecret, "extra-node", time.Hour)
	if err != nil {
		h.t.Fatalf("harness: sign agent token: %v", err)
	}

	agentCfg := config.AgentConfig{
		ServerAddr:  "passthrough://bufnet",
		Token:       agentToken,
		NodeName:    name,
		TLSInsecure: true,
	}

	agt := agent.NewAgent(agentCfg, "")
	agt.SetDialOverride(grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return h.lis.DialContext(ctx)
	}))

	agentCtx, agentCancel := context.WithCancel(context.Background())
	go func() {
		_ = agt.Run(agentCtx)
	}()

	h.t.Cleanup(agentCancel)

	// Wait for agent to register.
	nodeID := h.waitForNodeByName(name)
	return agt, nodeID
}

// ConnectAgentWithToken connects a new agent using the given JWT token and
// waits for it to register. Use this when the reconnecting agent must present
// the same token hash as the previously registered node.
func (h *Harness) ConnectAgentWithToken(name, token string) (*agent.Agent, string) {
	h.t.Helper()

	agentCfg := config.AgentConfig{
		ServerAddr:  "passthrough://bufnet",
		Token:       token,
		NodeName:    name,
		TLSInsecure: true,
	}

	agt := agent.NewAgent(agentCfg, "")
	agt.SetDialOverride(grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return h.lis.DialContext(ctx)
	}))

	agentCtx, agentCancel := context.WithCancel(context.Background())
	go func() {
		_ = agt.Run(agentCtx)
	}()

	h.t.Cleanup(agentCancel)

	nodeID := h.waitForNodeByName(name)
	return agt, nodeID
}

// ConnectAgentWithHandler connects a new agent with a custom TaskHandler.
func (h *Harness) ConnectAgentWithHandler(name string, handler agent.TaskHandler) (*agent.Agent, string) {
	h.t.Helper()

	agentToken, _, err := auth.SignAgentToken(h.opts.jwtSecret, "task-node", time.Hour)
	if err != nil {
		h.t.Fatalf("harness: sign agent token: %v", err)
	}

	agentCfg := config.AgentConfig{
		ServerAddr:  "passthrough://bufnet",
		Token:       agentToken,
		NodeName:    name,
		TLSInsecure: true,
	}

	agt := agent.NewAgent(agentCfg, "")
	agt.SetTaskHandler(handler)
	agt.SetDialOverride(grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return h.lis.DialContext(ctx)
	}))

	agentCtx, agentCancel := context.WithCancel(context.Background())
	go func() {
		_ = agt.Run(agentCtx)
	}()

	h.t.Cleanup(agentCancel)

	nodeID := h.waitForNodeByName(name)
	return agt, nodeID
}

// StopAgent cancels the primary agent's context, causing it to disconnect.
func (h *Harness) StopAgent() {
	if h.agentCancel != nil {
		h.agentCancel()
	}
}

// Close stops the server and agent.
func (h *Harness) Close() {
	if h.agentCancel != nil {
		h.agentCancel()
	}
	h.cancel()
	h.srv.GracefulStop()
}

// waitForAgent polls the registry until a node with the agent's name appears.
func (h *Harness) waitForAgent() string {
	h.t.Helper()
	return h.waitForNodeByName(h.opts.agentNodeName)
}

// waitForNodeByName polls the registry until a node with the given name appears.
func (h *Harness) waitForNodeByName(name string) string {
	h.t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		reg := h.srv.Registry()
		if reg == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		entry, ok := reg.LookupByName(name)
		if ok && entry.Status == registry.StatusOnline {
			return entry.NodeID
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("harness: agent %q did not appear in registry within 5s", name)
	return ""
}

// WaitForNodeStatus polls until the given node reaches the expected status.
func (h *Harness) WaitForNodeStatus(nodeID, status string) {
	h.t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entry, ok := h.Registry().Lookup(nodeID)
		if ok && entry.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	entry, ok := h.Registry().Lookup(nodeID)
	if !ok {
		h.t.Fatalf("harness: node %q not found in registry", nodeID)
	}
	h.t.Fatalf("harness: node %q status = %q, want %q", nodeID, entry.Status, status)
}

// authUnaryInterceptor returns a gRPC unary client interceptor that injects
// the authorization metadata on every call.
func authUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// authStreamInterceptor returns a gRPC stream client interceptor that injects
// the authorization metadata on every stream.
func authStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

// TaskSignal captures a task signal received by an agent.
type TaskSignal struct {
	TaskID   string
	TaskType controlpb.TaskType
}
