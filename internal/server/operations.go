// Package server provides the gRPC server and control plane implementation.
package server

import (
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlpb "github.com/garysng/axon/gen/proto/control"
	operationspb "github.com/garysng/axon/gen/proto/operations"
	"github.com/garysng/axon/pkg/audit"
	"github.com/garysng/axon/pkg/auth"
)

// OperationsService implements operationspb.OperationsServiceServer.
type OperationsService struct {
	operationspb.UnimplementedOperationsServiceServer
	router  *Router
	control *ControlService
	auditor audit.Auditor
}

var _ operationspb.OperationsServiceServer = (*OperationsService)(nil)

func newOperationsService(router *Router, control *ControlService, auditor audit.Auditor) *OperationsService {
	return &OperationsService{
		router:  router,
		control: control,
		auditor: auditor,
	}
}

// Exec routes the request to the target agent and returns a placeholder response
// until the agent data plane (C-9/C-10) is implemented.
func (s *OperationsService) Exec(req *operationspb.ExecRequest, stream grpc.ServerStreamingServer[operationspb.ExecOutput]) error {
	ctx := stream.Context()
	claims, _ := auth.ClaimsFromContext(ctx)

	entry, err := s.router.Route(ctx, req.GetNodeId())
	if err != nil {
		return err
	}

	taskID := uuid.NewString()
	if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_EXEC); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationExec, req.GetCommand(), audit.StatusError)
		return status.Errorf(codes.Internal, "operations: exec: send task signal: %v", err)
	}

	s.logAudit(claims, entry.NodeID, audit.OperationExec, req.GetCommand(), audit.StatusSuccess)

	return stream.Send(&operationspb.ExecOutput{
		Payload: &operationspb.ExecOutput_Exit{
			Exit: &operationspb.ExecExit{
				ExitCode: 1,
				Error:    "agent data plane not yet implemented",
			},
		},
	})
}

// Read routes the request to the target agent and returns a placeholder response.
func (s *OperationsService) Read(req *operationspb.ReadRequest, stream grpc.ServerStreamingServer[operationspb.ReadOutput]) error {
	ctx := stream.Context()
	claims, _ := auth.ClaimsFromContext(ctx)

	entry, err := s.router.Route(ctx, req.GetNodeId())
	if err != nil {
		return err
	}

	taskID := uuid.NewString()
	if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_READ); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationRead, req.GetPath(), audit.StatusError)
		return status.Errorf(codes.Internal, "operations: read: send task signal: %v", err)
	}

	s.logAudit(claims, entry.NodeID, audit.OperationRead, req.GetPath(), audit.StatusSuccess)

	return stream.Send(&operationspb.ReadOutput{
		Payload: &operationspb.ReadOutput_Error{
			Error: "agent data plane not yet implemented",
		},
	})
}

// Write reads the first WriteInput message (which must be a WriteHeader), routes
// to the target agent, and returns a placeholder response.
func (s *OperationsService) Write(stream grpc.ClientStreamingServer[operationspb.WriteInput, operationspb.WriteResponse]) error {
	ctx := stream.Context()
	claims, _ := auth.ClaimsFromContext(ctx)

	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "operations: write: read first message: %v", err)
	}

	hdr := first.GetHeader()
	if hdr == nil {
		return status.Error(codes.InvalidArgument, "operations: write: first message must be WriteHeader")
	}

	entry, err := s.router.Route(ctx, hdr.GetNodeId())
	if err != nil {
		return err
	}

	taskID := uuid.NewString()
	if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_WRITE); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationWrite, hdr.GetPath(), audit.StatusError)
		return status.Errorf(codes.Internal, "operations: write: send task signal: %v", err)
	}

	s.logAudit(claims, entry.NodeID, audit.OperationWrite, hdr.GetPath(), audit.StatusSuccess)

	return stream.SendAndClose(&operationspb.WriteResponse{
		Success: false,
		Error:   "agent data plane not yet implemented",
	})
}

// Forward reads the first TunnelData message (which must contain a TunnelOpen),
// routes to the target agent, and returns a placeholder response.
func (s *OperationsService) Forward(stream grpc.BidiStreamingServer[operationspb.TunnelData, operationspb.TunnelData]) error {
	ctx := stream.Context()
	claims, _ := auth.ClaimsFromContext(ctx)

	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "operations: forward: read first message: %v", err)
	}

	open := first.GetOpen()
	if open == nil {
		return status.Error(codes.InvalidArgument, "operations: forward: first message must contain TunnelOpen")
	}

	entry, err := s.router.Route(ctx, open.GetNodeId())
	if err != nil {
		return err
	}

	taskID := uuid.NewString()
	detail := open.GetNodeId()
	if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_FORWARD); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationForward, detail, audit.StatusError)
		return status.Errorf(codes.Internal, "operations: forward: send task signal: %v", err)
	}

	s.logAudit(claims, entry.NodeID, audit.OperationForward, detail, audit.StatusSuccess)

	// Send a close signal to indicate the forward channel is not yet implemented.
	return stream.Send(&operationspb.TunnelData{
		Close: true,
	})
}

// logAudit enqueues an audit entry if an auditor is configured.
func (s *OperationsService) logAudit(claims *auth.Claims, nodeID string, op audit.Operation, detail string, st audit.Status) {
	if s.auditor == nil {
		return
	}
	userID := ""
	if claims != nil {
		userID = claims.UserID
	}
	s.auditor.Log(audit.AuditEntry{
		Timestamp: time.Now(),
		UserID:    userID,
		NodeID:    nodeID,
		Operation: op,
		Detail:    detail,
		Status:    st,
	})
}
