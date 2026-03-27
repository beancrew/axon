// Package server provides the gRPC server and control plane implementation.
package server

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	_ "modernc.org/sqlite" // SQLite driver

	controlpb "github.com/garysng/axon/gen/proto/control"
	managementpb "github.com/garysng/axon/gen/proto/management"
	operationspb "github.com/garysng/axon/gen/proto/operations"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/audit"
	"github.com/garysng/axon/pkg/auth"
	autotls "github.com/garysng/axon/pkg/tls"
)

// ServerConfig holds configuration for the gRPC server.
type ServerConfig struct {
	ListenAddr        string
	TLSCertPath       string
	TLSKeyPath        string
	TLSAuto           bool   // auto-generate self-signed CA + server cert if no explicit cert is set
	TLSDir            string // directory for auto-generated TLS files; defaults to ~/.axon-server/tls
	JWTSecret         string
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	AuditDBPath       string // SQLite path for audit log; empty defaults to ":memory:"
	DataDBPath        string // SQLite path for registry/token/user stores; empty defaults to ":memory:"
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
	dataDB       *sql.DB // shared database; closed last in GracefulStop
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
	// Open the single shared database for registry, token, and user stores.
	dataDBPath := s.cfg.DataDBPath
	if dataDBPath == "" {
		dataDBPath = ":memory:"
	}
	db, err := sql.Open("sqlite", dataDBPath)
	if err != nil {
		return fmt.Errorf("server: open data db: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return fmt.Errorf("server: set WAL: %w", err)
	}
	s.dataDB = db

	// Build registry with its SQLite backing store.
	regStore, err := registry.NewSQLiteStoreFromDB(db)
	if err != nil {
		return fmt.Errorf("server: init registry store: %w", err)
	}
	s.registry = registry.NewRegistry(s.cfg.HeartbeatTimeout, regStore)
	s.control = newControlService(s.registry, s.cfg)

	// Initialize token management (store + in-memory revocation checker).
	tokenStore, err := auth.NewTokenStoreFromDB(db)
	if err != nil {
		return fmt.Errorf("server: init token store: %w", err)
	}
	s.tokenStore = tokenStore

	tokenChecker, err := auth.NewTokenChecker(tokenStore)
	if err != nil {
		return fmt.Errorf("server: init token checker: %w", err)
	}
	s.tokenChecker = tokenChecker

	// Initialize join token store.
	joinTokenStore, err := auth.NewJoinTokenStoreFromDB(db)
	if err != nil {
		return fmt.Errorf("server: init join token store: %w", err)
	}

	// effectiveTLSDir is set when auto-TLS is active so the management service
	// can serve the CA cert to joining agents.
	var effectiveTLSDir string

	// Auto-TLS: generate a self-signed CA + server cert when enabled and no
	// explicit cert/key paths are configured.
	if s.cfg.TLSAuto && s.cfg.TLSCertPath == "" {
		tlsDir := s.cfg.TLSDir
		if tlsDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("server: resolve home dir for TLS: %w", err)
			}
			tlsDir = filepath.Join(home, ".axon-server", "tls")
		}
		effectiveTLSDir = tlsDir
		// Use the listen address host as the server cert CN/SAN when it is a
		// specific hostname rather than a wildcard bind address.
		hostname := "localhost"
		if host, _, err := net.SplitHostPort(s.cfg.ListenAddr); err == nil &&
			host != "" && host != "0.0.0.0" && host != "::" {
			hostname = host
		}
		caCertPath, serverCertPath, serverKeyPath, generated, err := autotls.EnsureTLS(tlsDir, hostname)
		if err != nil {
			return fmt.Errorf("server: auto-TLS: %w", err)
		}
		s.cfg.TLSCertPath = serverCertPath
		s.cfg.TLSKeyPath = serverKeyPath
		if generated {
			fp, _ := autotls.CAFingerprint(caCertPath)
			log.Printf("server: auto-TLS: generated CA cert %s (SHA-256: %s)", caCertPath, fp)
		} else {
			log.Printf("server: auto-TLS: using existing CA cert %s", caCertPath)
		}
	}

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
	mgmt := newManagementService(s.registry, s.cfg.JWTSecret, s.tokenStore, s.tokenChecker, joinTokenStore, effectiveTLSDir, s.cfg.HeartbeatInterval)

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

// GracefulStop shuts down the gRPC server gracefully and releases all resources.
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
	if s.registry != nil {
		_ = s.registry.Close()
	}
	// Close the shared database last, after all stores have been shut down.
	if s.dataDB != nil {
		_ = s.dataDB.Close()
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

	// All methods require a valid JWT, except JoinAgent (unauthenticated enrolment)
	// and ControlService/Connect (validates its own token internally).
	opts = append(opts,
		grpc.ChainUnaryInterceptor(skipJoinAgentUnaryInterceptor(s.cfg.JWTSecret, checker)),
		grpc.ChainStreamInterceptor(skipConnectStreamInterceptor(s.cfg.JWTSecret, checker)),
	)

	return opts, nil
}

// skipJoinAgentUnaryInterceptor wraps auth.UnaryInterceptorWithChecker but bypasses
// JWT auth for ManagementService/JoinAgent (the unauthenticated agent enrolment endpoint).
func skipJoinAgentUnaryInterceptor(secret string, checker *auth.TokenChecker) grpc.UnaryServerInterceptor {
	inner := auth.UnaryInterceptorWithChecker(secret, checker)
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		if info.FullMethod == managementpb.ManagementService_JoinAgent_FullMethodName ||
			info.FullMethod == managementpb.ManagementService_Login_FullMethodName {
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
