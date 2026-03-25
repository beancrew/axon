package server

import (
	"context"
	"database/sql"
	"fmt"
	"net"

	"google.golang.org/grpc"

	_ "modernc.org/sqlite" // SQLite driver

	controlpb "github.com/garysng/axon/gen/proto/control"
	managementpb "github.com/garysng/axon/gen/proto/management"
	operationspb "github.com/garysng/axon/gen/proto/operations"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/audit"
	"github.com/garysng/axon/pkg/auth"
)

// ServeListenerReady starts the server on lis and closes the ready channel
// once all internal components are initialised. This avoids the data race
// between serve() writing s.registry and test code reading it via Registry().
func (s *Server) ServeListenerReady(ctx context.Context, lis net.Listener, ready chan<- struct{}) error {
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

	// Initialize token management for tests.
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

	// Initialize user store and bootstrap config users for tests.
	userStore, err := auth.NewUserStoreFromDB(db)
	if err != nil {
		return fmt.Errorf("server: init user store: %w", err)
	}
	s.userStore = userStore
	for i := range s.cfg.Users {
		_, _ = userStore.InsertIfAbsent(&s.cfg.Users[i])
	}

	// Initialize join token store for tests.
	joinTokenStore, err := auth.NewJoinTokenStoreFromDB(db)
	if err != nil {
		return fmt.Errorf("server: init join token store: %w", err)
	}

	opts, err := s.buildServerOptions(tokenChecker)
	if err != nil {
		return err
	}
	s.grpc = grpc.NewServer(opts...)

	auditDBPath := s.cfg.AuditDBPath
	if auditDBPath == "" {
		auditDBPath = ":memory:"
	}
	auditStore, err := audit.NewStore(auditDBPath)
	if err != nil {
		return fmt.Errorf("server: init audit store: %w", err)
	}
	s.auditWriter = audit.NewWriter(auditStore, 256)

	router := newRouter(s.registry)
	bridge := newTaskBridge()
	ops := newOperationsService(router, s.control, bridge, s.auditWriter)
	agentOps := newAgentOpsService(bridge)
	mgmt := newManagementService(s.registry, s.userStore, s.cfg.JWTSecret, s.tokenStore, s.tokenChecker, joinTokenStore, "", s.cfg.HeartbeatInterval)

	controlpb.RegisterControlServiceServer(s.grpc, s.control)
	operationspb.RegisterOperationsServiceServer(s.grpc, ops)
	operationspb.RegisterAgentOpsServiceServer(s.grpc, agentOps)
	managementpb.RegisterManagementServiceServer(s.grpc, mgmt)

	s.registry.StartMonitor(ctx)

	// Signal that initialisation is complete — callers can now safely
	// access Registry() and other server state without a data race.
	close(ready)

	go func() {
		<-ctx.Done()
		s.grpc.GracefulStop()
	}()

	if err := s.grpc.Serve(lis); err != nil {
		return fmt.Errorf("server: serve: %w", err)
	}
	return nil
}
