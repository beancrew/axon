package server

import (
	"context"
	"io"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationspb "github.com/garysng/axon/gen/proto/operations"
)

// AgentOpsServiceImpl implements operationspb.AgentOpsServiceServer.
// Agents call HandleTask to fulfill dispatched tasks.
type AgentOpsServiceImpl struct {
	operationspb.UnimplementedAgentOpsServiceServer
	bridge *taskBridge
}

var _ operationspb.AgentOpsServiceServer = (*AgentOpsServiceImpl)(nil)

func newAgentOpsService(bridge *taskBridge) *AgentOpsServiceImpl {
	return &AgentOpsServiceImpl{bridge: bridge}
}

// HandleTask is a bidirectional stream opened by the Agent after receiving a TaskSignal.
// First message from Agent must contain task_id for handshake.
// Server then relays data between the CLI stream and the Agent stream via the bridge.
func (s *AgentOpsServiceImpl) HandleTask(stream grpc.BidiStreamingServer[operationspb.TaskDataUp, operationspb.TaskDataDown]) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// Read first message — handshake with task_id.
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "agent_ops: read handshake: %v", err)
	}
	taskID := first.GetTaskId()
	if taskID == "" {
		return status.Error(codes.InvalidArgument, "agent_ops: first message must contain task_id")
	}

	slot, ok := s.bridge.Attach(taskID)
	if !ok {
		return status.Errorf(codes.NotFound, "agent_ops: no pending task %q", taskID)
	}

	log.Printf("agent_ops: agent attached to task %s (type=%v)", taskID, slot.taskType)

	// If the first message also contains a payload, forward it.
	if first.GetExecOutput() != nil || first.GetReadOutput() != nil ||
		first.GetWriteResponse() != nil || first.GetTunnelData() != nil {
		select {
		case slot.up <- first:
		case <-slot.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	errCh := make(chan error, 2)

	// Agent → Server (up): read from Agent stream, put on slot.up.
	// Close slot.up when Agent finishes sending (EOF) so the CLI relay detects completion.
	go func() {
		defer close(slot.up)
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
			case slot.up <- msg:
			case <-slot.done:
				errCh <- nil
				return
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()

	// Server → Agent (down): read from slot.down, send to Agent stream.
	go func() {
		for {
			select {
			case msg, ok := <-slot.down:
				if !ok {
					// CLI done sending (channel closed).
					errCh <- nil
					return
				}
				if err := stream.Send(msg); err != nil {
					errCh <- err
					return
				}
			case <-slot.done:
				errCh <- nil
				return
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()

	// Wait for the first goroutine to finish, then cancel the other.
	err = <-errCh
	cancel()
	return err
}
