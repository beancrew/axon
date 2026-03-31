// Package server provides the gRPC server and control plane implementation.
package server

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlpb "github.com/beancrew/axon/gen/proto/control"
	operationspb "github.com/beancrew/axon/gen/proto/operations"
	"github.com/beancrew/axon/pkg/audit"
	"github.com/beancrew/axon/pkg/auth"
)

const agentAttachTimeout = 30 * time.Second

// OperationsService implements operationspb.OperationsServiceServer.
type OperationsService struct {
	operationspb.UnimplementedOperationsServiceServer
	router  *Router
	control *ControlService
	bridge  *taskBridge
	auditor audit.Auditor
}

var _ operationspb.OperationsServiceServer = (*OperationsService)(nil)

func newOperationsService(router *Router, control *ControlService, bridge *taskBridge, auditor audit.Auditor) *OperationsService {
	return &OperationsService{
		router:  router,
		control: control,
		bridge:  bridge,
		auditor: auditor,
	}
}

// Exec routes the request to the target agent and bridges the CLI↔Agent streams.
func (s *OperationsService) Exec(req *operationspb.ExecRequest, stream grpc.ServerStreamingServer[operationspb.ExecOutput]) error {
	ctx := stream.Context()
	claims, _ := auth.ClaimsFromContext(ctx)

	entry, err := s.router.Route(ctx, req.GetNodeId())
	if err != nil {
		return err
	}

	taskID := uuid.NewString()
	slot := s.bridge.Create(taskID, controlpb.TaskType_TASK_EXEC)
	defer s.bridge.Remove(taskID)

	if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_EXEC); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationExec, req.GetCommand(), audit.StatusError)
		return status.Errorf(codes.Internal, "operations: exec: send task signal: %v", err)
	}

	if err := s.bridge.WaitAttach(ctx, slot, agentAttachTimeout); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationExec, req.GetCommand(), audit.StatusError)
		return status.Errorf(codes.DeadlineExceeded, "operations: exec: %v", err)
	}

	// Send the request to the Agent.
	slot.down <- &operationspb.TaskDataDown{
		TaskId:  taskID,
		Payload: &operationspb.TaskDataDown_ExecRequest{ExecRequest: req},
	}

	s.logAudit(claims, entry.NodeID, audit.OperationExec, req.GetCommand(), audit.StatusSuccess)

	// Relay Agent output to CLI.
	for {
		select {
		case msg, ok := <-slot.up:
			if !ok {
				return nil
			}
			if out := msg.GetExecOutput(); out != nil {
				if err := stream.Send(out); err != nil {
					return err
				}
				if out.GetExit() != nil {
					return nil
				}
			}
		case <-slot.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Read routes the request to the target agent and bridges the streams.
func (s *OperationsService) Read(req *operationspb.ReadRequest, stream grpc.ServerStreamingServer[operationspb.ReadOutput]) error {
	ctx := stream.Context()
	claims, _ := auth.ClaimsFromContext(ctx)

	entry, err := s.router.Route(ctx, req.GetNodeId())
	if err != nil {
		return err
	}

	taskID := uuid.NewString()
	slot := s.bridge.Create(taskID, controlpb.TaskType_TASK_READ)
	defer s.bridge.Remove(taskID)

	if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_READ); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationRead, req.GetPath(), audit.StatusError)
		return status.Errorf(codes.Internal, "operations: read: send task signal: %v", err)
	}

	if err := s.bridge.WaitAttach(ctx, slot, agentAttachTimeout); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationRead, req.GetPath(), audit.StatusError)
		return status.Errorf(codes.DeadlineExceeded, "operations: read: %v", err)
	}

	// Send the request to the Agent.
	slot.down <- &operationspb.TaskDataDown{
		TaskId:  taskID,
		Payload: &operationspb.TaskDataDown_ReadRequest{ReadRequest: req},
	}

	s.logAudit(claims, entry.NodeID, audit.OperationRead, req.GetPath(), audit.StatusSuccess)

	// Relay Agent output to CLI. The loop terminates when:
	// - Agent sends an error message
	// - Agent closes its stream (slot.up is closed by agent_ops)
	// - Task is done or context is cancelled
	for {
		select {
		case msg, ok := <-slot.up:
			if !ok {
				// Agent closed stream — read complete.
				return nil
			}
			if out := msg.GetReadOutput(); out != nil {
				if err := stream.Send(out); err != nil {
					return err
				}
				if out.GetError() != "" {
					return nil
				}
			}
		case <-slot.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Write reads WriteInput from CLI, relays to Agent, and returns the response.
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
	slot := s.bridge.Create(taskID, controlpb.TaskType_TASK_WRITE)
	defer s.bridge.Remove(taskID)

	if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_WRITE); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationWrite, hdr.GetPath(), audit.StatusError)
		return status.Errorf(codes.Internal, "operations: write: send task signal: %v", err)
	}

	if err := s.bridge.WaitAttach(ctx, slot, agentAttachTimeout); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationWrite, hdr.GetPath(), audit.StatusError)
		return status.Errorf(codes.DeadlineExceeded, "operations: write: %v", err)
	}

	// Send header to Agent.
	slot.down <- &operationspb.TaskDataDown{
		TaskId:  taskID,
		Payload: &operationspb.TaskDataDown_WriteInput{WriteInput: first},
	}

	s.logAudit(claims, entry.NodeID, audit.OperationWrite, hdr.GetPath(), audit.StatusSuccess)

	// Relay remaining CLI data to Agent.
	// Close slot.down on EOF so Agent's recvData unblocks.
	go func() {
		defer close(slot.down)
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			select {
			case slot.down <- &operationspb.TaskDataDown{
				TaskId:  taskID,
				Payload: &operationspb.TaskDataDown_WriteInput{WriteInput: msg},
			}:
			case <-slot.done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for Agent response.
	select {
	case msg, ok := <-slot.up:
		if !ok {
			return status.Error(codes.Internal, "operations: write: agent closed stream without response")
		}
		if resp := msg.GetWriteResponse(); resp != nil {
			return stream.SendAndClose(resp)
		}
		return status.Error(codes.Internal, "operations: write: unexpected agent response")
	case <-slot.done:
		return status.Error(codes.Internal, "operations: write: task ended unexpectedly")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Forward reads TunnelData from CLI, relays bidirectionally with Agent.
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
	slot := s.bridge.Create(taskID, controlpb.TaskType_TASK_FORWARD)
	defer s.bridge.Remove(taskID)

	if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_FORWARD); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationForward, detail, audit.StatusError)
		return status.Errorf(codes.Internal, "operations: forward: send task signal: %v", err)
	}

	if err := s.bridge.WaitAttach(ctx, slot, agentAttachTimeout); err != nil {
		s.logAudit(claims, entry.NodeID, audit.OperationForward, detail, audit.StatusError)
		return status.Errorf(codes.DeadlineExceeded, "operations: forward: %v", err)
	}

	// Send first TunnelData (with TunnelOpen) to Agent.
	slot.down <- &operationspb.TaskDataDown{
		TaskId:  taskID,
		Payload: &operationspb.TaskDataDown_TunnelData{TunnelData: first},
	}

	s.logAudit(claims, entry.NodeID, audit.OperationForward, detail, audit.StatusSuccess)

	fwdCtx, fwdCancel := context.WithCancel(ctx)
	defer fwdCancel()

	errCh := make(chan error, 2)

	// CLI → Agent: read from CLI stream, put on slot.down.
	go func() {
		defer close(slot.down)
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				errCh <- nil
				return
			}
			if err != nil {
				errCh <- err
				return
			}
			select {
			case slot.down <- &operationspb.TaskDataDown{
				TaskId:  taskID,
				Payload: &operationspb.TaskDataDown_TunnelData{TunnelData: msg},
			}:
			case <-slot.done:
				errCh <- nil
				return
			case <-fwdCtx.Done():
				errCh <- fwdCtx.Err()
				return
			}
		}
	}()

	// Agent → CLI: read from slot.up, send to CLI stream.
	go func() {
		for {
			select {
			case msg, ok := <-slot.up:
				if !ok {
					errCh <- nil
					return
				}
				if td := msg.GetTunnelData(); td != nil {
					if err := stream.Send(td); err != nil {
						errCh <- err
						return
					}
					if td.GetClose() {
						errCh <- nil
						return
					}
				}
			case <-slot.done:
				errCh <- nil
				return
			case <-fwdCtx.Done():
				errCh <- fwdCtx.Err()
				return
			}
		}
	}()

	// Wait for first goroutine to finish, cancel the other.
	err = <-errCh
	fwdCancel()
	return err
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
