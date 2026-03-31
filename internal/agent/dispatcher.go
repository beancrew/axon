package agent

import (
	"context"
	"io"
	"log"

	"google.golang.org/grpc"

	controlpb "github.com/beancrew/axon/gen/proto/control"
	operationspb "github.com/beancrew/axon/gen/proto/operations"
)

// Dispatcher opens data plane streams to the server to fulfill dispatched tasks.
type Dispatcher struct {
	conn  *grpc.ClientConn
	execH *ExecHandler
	fileH *FileIOHandler
	fwdH  *ForwardHandler
}

// NewDispatcher creates a Dispatcher using the given gRPC connection.
func NewDispatcher(conn *grpc.ClientConn) *Dispatcher {
	return &Dispatcher{
		conn:  conn,
		execH: &ExecHandler{},
		fileH: &FileIOHandler{},
		fwdH:  &ForwardHandler{},
	}
}

// HandleTask opens a HandleTask bidi stream, handshakes with task_id,
// receives the request, and dispatches to the appropriate handler.
func (d *Dispatcher) HandleTask(ctx context.Context, taskID string, taskType controlpb.TaskType) {
	client := operationspb.NewAgentOpsServiceClient(d.conn)
	stream, err := client.HandleTask(ctx)
	if err != nil {
		log.Printf("dispatcher: open stream for task %s: %v", taskID, err)
		return
	}

	// Handshake: send task_id.
	if err := stream.Send(&operationspb.TaskDataUp{TaskId: taskID}); err != nil {
		log.Printf("dispatcher: send handshake for task %s: %v", taskID, err)
		return
	}

	// Receive request from server.
	msg, err := stream.Recv()
	if err != nil {
		log.Printf("dispatcher: recv request for task %s: %v", taskID, err)
		return
	}

	log.Printf("dispatcher: handling task %s (type=%v)", taskID, taskType)

	switch taskType {
	case controlpb.TaskType_TASK_EXEC:
		d.handleExec(ctx, stream, msg, taskID)
	case controlpb.TaskType_TASK_READ:
		d.handleRead(ctx, stream, msg, taskID)
	case controlpb.TaskType_TASK_WRITE:
		d.handleWrite(stream, msg, taskID)
	case controlpb.TaskType_TASK_FORWARD:
		d.handleForward(ctx, stream, msg, taskID)
	default:
		log.Printf("dispatcher: unknown task type %v for task %s", taskType, taskID)
	}

	_ = stream.CloseSend()
}

func (d *Dispatcher) handleExec(ctx context.Context, stream grpc.BidiStreamingClient[operationspb.TaskDataUp, operationspb.TaskDataDown], msg *operationspb.TaskDataDown, taskID string) {
	req := msg.GetExecRequest()
	if req == nil {
		log.Printf("dispatcher: task %s: expected ExecRequest", taskID)
		return
	}

	d.execH.Handle(ctx, req, func(out *operationspb.ExecOutput) error {
		return stream.Send(&operationspb.TaskDataUp{
			TaskId:  taskID,
			Payload: &operationspb.TaskDataUp_ExecOutput{ExecOutput: out},
		})
	})
}

func (d *Dispatcher) handleRead(ctx context.Context, stream grpc.BidiStreamingClient[operationspb.TaskDataUp, operationspb.TaskDataDown], msg *operationspb.TaskDataDown, taskID string) {
	req := msg.GetReadRequest()
	if req == nil {
		log.Printf("dispatcher: task %s: expected ReadRequest", taskID)
		return
	}

	d.fileH.HandleRead(req, func(out *operationspb.ReadOutput) error {
		// Check for cancellation before each send.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return stream.Send(&operationspb.TaskDataUp{
			TaskId:  taskID,
			Payload: &operationspb.TaskDataUp_ReadOutput{ReadOutput: out},
		})
	})
}

func (d *Dispatcher) handleWrite(stream grpc.BidiStreamingClient[operationspb.TaskDataUp, operationspb.TaskDataDown], msg *operationspb.TaskDataDown, taskID string) {
	firstInput := msg.GetWriteInput()
	if firstInput == nil {
		log.Printf("dispatcher: task %s: expected WriteInput", taskID)
		return
	}

	// HandleWrite expects recvHeader and recvData callbacks.
	// The first message from server contains the WriteHeader.
	var headerConsumed bool
	recvHeader := func() (*operationspb.WriteHeader, error) {
		if !headerConsumed {
			headerConsumed = true
			return firstInput.GetHeader(), nil
		}
		return nil, io.EOF
	}

	recvData := func() ([]byte, error) {
		msg, err := stream.Recv()
		if err != nil {
			return nil, err
		}
		wi := msg.GetWriteInput()
		if wi == nil {
			return nil, io.EOF
		}
		return wi.GetData(), nil
	}

	resp := d.fileH.HandleWrite(recvHeader, recvData)

	// Send response back to server.
	_ = stream.Send(&operationspb.TaskDataUp{
		TaskId:  taskID,
		Payload: &operationspb.TaskDataUp_WriteResponse{WriteResponse: resp},
	})
}

func (d *Dispatcher) handleForward(ctx context.Context, stream grpc.BidiStreamingClient[operationspb.TaskDataUp, operationspb.TaskDataDown], msg *operationspb.TaskDataDown, taskID string) {
	td := msg.GetTunnelData()
	if td == nil || td.GetOpen() == nil {
		log.Printf("dispatcher: task %s: expected TunnelData with TunnelOpen", taskID)
		return
	}

	_ = d.fwdH.Handle(ctx, td.GetOpen().GetRemotePort(), td.GetConnectionId(),
		// recv: get next TunnelData from server.
		func() (*operationspb.TunnelData, error) {
			msg, err := stream.Recv()
			if err != nil {
				return nil, err
			}
			return msg.GetTunnelData(), nil
		},
		// send: send TunnelData back to server.
		func(out *operationspb.TunnelData) error {
			return stream.Send(&operationspb.TaskDataUp{
				TaskId:  taskID,
				Payload: &operationspb.TaskDataUp_TunnelData{TunnelData: out},
			})
		},
	)
}
