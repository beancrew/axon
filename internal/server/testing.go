package server

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	controlpb "github.com/garysng/axon/gen/proto/control"
	managementpb "github.com/garysng/axon/gen/proto/management"
	operationspb "github.com/garysng/axon/gen/proto/operations"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/audit"
)

// ServeListenerReady starts the server on lis and closes the ready channel
// once all internal components are initialised. This avoids the data race
// between serve() writing s.registry and test code reading it via Registry().
func (s *Server) ServeListenerReady(ctx context.Context, lis net.Listener, ready chan<- struct{}) error {
	// Pre-initialise the components that serve() would create, so they are
	// visible to callers *before* the goroutine starts gRPC serving.
	opts, err := s.buildServerOptions()
	if err != nil {
		return err
	}

	s.grpc = grpc.NewServer(opts...)

	s.registry = registry.NewRegistry(s.cfg.HeartbeatTimeout)
	s.control = newControlService(s.registry, s.cfg)

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
	mgmt := newManagementService(s.registry, s.cfg.Users, s.cfg.JWTSecret)

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
