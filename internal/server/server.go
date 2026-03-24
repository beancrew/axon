// Package server provides the gRPC server and control plane implementation.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	controlpb "github.com/garysng/axon/gen/proto/control"
	managementpb "github.com/garysng/axon/gen/proto/management"
	operationspb "github.com/garysng/axon/gen/proto/operations"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/audit"
	"github.com/garysng/axon/pkg/auth"
)

// ServerConfig holds configuration for the gRPC server.
type ServerConfig struct {
	ListenAddr        string
	TLSCertPath       string
	TLSKeyPath        string
	JWTSecret         string
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	AuditDBPath       string      // SQLite path; empty or ":memory:" for in-process
	Users             []UserEntry // CLI user credentials for Login
}

// Server wraps a gRPC server with its dependencies.
type Server struct {
	cfg          ServerConfig
	grpc         *grpc.Server
	registry     *registry.Registry
	control      *ControlService
	auditWriter  *audit.Writer
	tokenStore   *auth.TokenStore
	tokenChecker *auth.TokenChecker
}

// NewServer creates a new Server with the given configuration.
func NewServer(cfg ServerConfig) *Server {
	return &Server{cfg: cfg}
}

// Start initialises and starts the gRPC server, blocking until ctx is
// cancelled or the listener fails. It registers all services before
// accepting connections.
func (s *Server) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("server: listen %s: %w", s.cfg.ListenAddr, err)
	}
	return s.serve(ctx, lis)
}

// serve is the internal implementation shared by Start and test helpers
// that supply their own net.Listener (e.g. bufconn).
func (s *Server) serve(ctx context.Context, lis net.Listener) error {
	// Build registry and control service.
	s.registry = registry.NewRegistry(s.cfg.HeartbeatTimeout)
	s.control = newControlService(s.registry, s.cfg)

	// Initialize token management (store + in-memory revocation checker).
	tokenStore, err := auth.NewTokenStore(":memory:")
	if err != nil {
		return fmt.Errorf("server: init token store: %w", err)
	}
	s.tokenStore = tokenStore

	tokenChecker, err := auth.NewTokenChecker(tokenStore)
	if err != nil {
		return fmt.Errorf("server: init token checker: %w", err)
	}
	s.tokenChecker = tokenChecker

	// Build gRPC server with interceptors that check revoked tokens.
	opts, err := s.buildServerOptions(tokenChecker)
	if err != nil {
		return err
	}
	s.grpc = grpc.NewServer(opts...)

	// Initialize audit.
	auditDBPath := s.cfg.AuditDBPath
	if auditDBPath == "" {
		auditDBPath = ":memory:"
	}
	auditStore, err := audit.NewStore(auditDBPath)
	if err != nil {
		return fmt.Errorf("server: init audit store: %w", err)
	}
	s.auditWriter = audit.NewWriter(auditStore, 256)

	// Build router, bridge, and services.
	router := newRouter(s.registry)
	bridge := newTaskBridge()
	ops := newOperationsService(router, s.control, bridge, s.auditWriter)
	agentOps := newAgentOpsService(bridge)
	mgmt := newManagementService(s.registry, s.cfg.Users, s.cfg.JWTSecret, s.tokenStore, s.tokenChecker)

	controlpb.RegisterControlServiceServer(s.grpc, s.control)
	operationspb.RegisterOperationsServiceServer(s.grpc, ops)
	operationspb.RegisterAgentOpsServiceServer(s.grpc, agentOps)
	managementpb.RegisterManagementServiceServer(s.grpc, mgmt)

	// Start heartbeat monitor; it stops when ctx is cancelled.
	s.registry.StartMonitor(ctx)

	// Stop the gRPC server when the context is cancelled.
	go func() {
		<-ctx.Done()
		s.grpc.GracefulStop()
	}()

	if err := s.grpc.Serve(lis); err != nil {
		// GracefulStop causes Serve to return nil; any other error is real.
		return fmt.Errorf("server: serve: %w", err)
	}
	return nil
}

// GracefulStop shuts down the gRPC server gracefully and flushes the audit log.
func (s *Server) GracefulStop() {
	if s.grpc != nil {
		s.grpc.GracefulStop()
	}
	if s.auditWriter != nil {
		_ = s.auditWriter.Close()
	}
	if s.tokenStore != nil {
		_ = s.tokenStore.Close()
	}
}

// Registry returns the node registry used by this server.
func (s *Server) Registry() *registry.Registry {
	return s.registry
}

// buildServerOptions constructs the gRPC server options, including TLS and auth
// interceptors. When checker is non-nil, revoked tokens are rejected.
func (s *Server) buildServerOptions(checker *auth.TokenChecker) ([]grpc.ServerOption, error) {
	var opts []grpc.ServerOption

	// TLS is optional; only enabled when both cert and key paths are provided.
	if s.cfg.TLSCertPath != "" && s.cfg.TLSKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(s.cfg.TLSCertPath, s.cfg.TLSKeyPath)
		if err != nil {
			return nil, fmt.Errorf("server: load TLS keypair: %w", err)
		}
		creds := credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})
		opts = append(opts, grpc.Creds(creds))
	}

	// The ControlService/Connect stream handles its own token validation, and
	// ManagementService/Login is the unauthenticated entry point. All other
	// methods require a valid JWT (with revocation check when checker != nil).
	opts = append(opts,
		grpc.ChainUnaryInterceptor(skipLoginUnaryInterceptor(s.cfg.JWTSecret, checker)),
		grpc.ChainStreamInterceptor(skipConnectStreamInterceptor(s.cfg.JWTSecret, checker)),
	)

	return opts, nil
}

// skipLoginUnaryInterceptor wraps auth.UnaryInterceptorWithChecker but bypasses
// JWT auth for ManagementService/Login (which is the unauthenticated login endpoint).
func skipLoginUnaryInterceptor(secret string, checker *auth.TokenChecker) grpc.UnaryServerInterceptor {
	inner := auth.UnaryInterceptorWithChecker(secret, checker)
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if info.FullMethod == managementpb.ManagementService_Login_FullMethodName {
			return handler(ctx, req)
		}
		return inner(ctx, req, info, handler)
	}
}

// skipConnectStreamInterceptor wraps auth.StreamInterceptorWithChecker but
// bypasses JWT auth for the ControlService/Connect method (which validates its
// own token inside the handler).
func skipConnectStreamInterceptor(secret string, checker *auth.TokenChecker) grpc.StreamServerInterceptor {
	inner := auth.StreamInterceptorWithChecker(secret, checker)
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if info.FullMethod == controlpb.ControlService_Connect_FullMethodName {
			return handler(srv, ss)
		}
		return inner(srv, ss, info, handler)
	}
}
