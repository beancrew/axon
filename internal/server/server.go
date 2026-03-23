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
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/auth"
)

// ServerConfig holds configuration for the gRPC server.
type ServerConfig struct {
	ListenAddr         string
	TLSCertPath        string
	TLSKeyPath         string
	JWTSecret          string
	HeartbeatInterval  time.Duration
	HeartbeatTimeout   time.Duration
}

// Server wraps a gRPC server with its dependencies.
type Server struct {
	cfg      ServerConfig
	grpc     *grpc.Server
	registry *registry.Registry
	control  *ControlService
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
	opts, err := s.buildServerOptions()
	if err != nil {
		return err
	}

	s.grpc = grpc.NewServer(opts...)

	// Build registry and control service.
	s.registry = registry.NewRegistry(s.cfg.HeartbeatTimeout)
	s.control = newControlService(s.registry, s.cfg)

	controlpb.RegisterControlServiceServer(s.grpc, s.control)

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

// GracefulStop shuts down the gRPC server gracefully.
func (s *Server) GracefulStop() {
	if s.grpc != nil {
		s.grpc.GracefulStop()
	}
}

// Registry returns the node registry used by this server.
func (s *Server) Registry() *registry.Registry {
	return s.registry
}

// buildServerOptions constructs the gRPC server options, including TLS and auth
// interceptors.
func (s *Server) buildServerOptions() ([]grpc.ServerOption, error) {
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

	// The ControlService/Connect stream handles its own token validation, so the
	// generic auth interceptors protect all other methods.
	opts = append(opts,
		grpc.ChainUnaryInterceptor(auth.UnaryInterceptor(s.cfg.JWTSecret)),
		grpc.ChainStreamInterceptor(skipConnectStreamInterceptor(s.cfg.JWTSecret)),
	)

	return opts, nil
}

// skipConnectStreamInterceptor wraps auth.StreamInterceptor but bypasses JWT
// auth for the ControlService/Connect method (which validates its own token
// inside the handler).
func skipConnectStreamInterceptor(secret string) grpc.StreamServerInterceptor {
	inner := auth.StreamInterceptor(secret)
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
