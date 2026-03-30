package server

import (
	"context"
	"io"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
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

	upErrCh := make(chan error, 1)
	downErrCh := make(chan error, 1)

	// Agent → Server (up): read from Agent stream, put on slot.up.
	// Close slot.up when Agent finishes sending (EOF) so the CLI relay detects completion.
	go func() {
		defer close(slot.up)
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				upErrCh <- nil
				return
			}
			if err != nil {
				upErrCh <- err
				return
			}
			select {
			case slot.up <- msg:
			case <-slot.done:
				upErrCh <- nil
				return
			case <-ctx.Done():
				upErrCh <- ctx.Err()
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
					// CLI done sending (channel closed). This is normal for
					// write ops — Agent still needs to process and respond.
					downErrCh <- nil
					return
				}
				if err := stream.Send(msg); err != nil {
					downErrCh <- err
					return
				}
			case <-slot.done:
				downErrCh <- nil
				return
			case <-ctx.Done():
				downErrCh <- ctx.Err()
				return
			}
		}
	}()

	// Wait for both goroutines. The "down" goroutine finishing (CLI done sending)
	// is normal and must NOT cause early termination — the Agent may still need to
	// send its response via the "up" goroutine (e.g. WriteResponse after processing).
	// Only a real error from either side triggers early cancellation.
	var firstErr error
	for i := 0; i < 2; i++ {
		select {
		case err := <-upErrCh:
			if err != nil && firstErr == nil {
				firstErr = err
				cancel() // cancel the other goroutine
			}
		case err := <-downErrCh:
			if err != nil && firstErr == nil {
				firstErr = err
				cancel() // cancel the other goroutine
			}
		}
	}
	return firstErr
}
