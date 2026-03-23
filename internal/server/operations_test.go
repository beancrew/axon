package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationspb "github.com/garysng/axon/gen/proto/operations"
)

func TestOperations_Exec(t *testing.T) {
	env := newFullTestEnv(t, nil)
	nodeID := connectAgent(t, env, "exec-node")

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Exec(ctx, &operationspb.ExecRequest{
		NodeId:  nodeID,
		Command: "ls -la",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	exit := resp.GetExit()
	if exit == nil {
		t.Fatal("expected ExecExit payload")
	}
	if exit.Error == "" {
		t.Error("expected placeholder error message")
	}
}

func TestOperations_Exec_NodeNotFound(t *testing.T) {
	env := newFullTestEnv(t, nil)

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Exec(ctx, &operationspb.ExecRequest{
		NodeId:  "nonexistent",
		Command: "ls",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("got code %v, want %v", st.Code(), codes.NotFound)
	}
}

func TestOperations_Exec_PermissionDenied(t *testing.T) {
	env := newFullTestEnv(t, nil)
	connectAgent(t, env, "restricted-node")

	oc := operationspb.NewOperationsServiceClient(env.conn)
	// User only has access to "other-node", not the registered one.
	ctx := authedCtx(t, "user1", []string{"other-node"})

	stream, err := oc.Exec(ctx, &operationspb.ExecRequest{
		NodeId:  "restricted-node",
		Command: "ls",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for permission denied")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Errorf("got code %v, want %v", st.Code(), codes.PermissionDenied)
	}
}

func TestOperations_Read(t *testing.T) {
	env := newFullTestEnv(t, nil)
	nodeID := connectAgent(t, env, "read-node")

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Read(ctx, &operationspb.ReadRequest{
		NodeId: nodeID,
		Path:   "/etc/hosts",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	errMsg := resp.GetError()
	if errMsg == "" {
		t.Error("expected placeholder error message")
	}
}

func TestOperations_Write(t *testing.T) {
	env := newFullTestEnv(t, nil)
	nodeID := connectAgent(t, env, "write-node")

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Write(ctx)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Send WriteHeader.
	if err := stream.Send(&operationspb.WriteInput{
		Payload: &operationspb.WriteInput_Header{
			Header: &operationspb.WriteHeader{
				NodeId: nodeID,
				Path:   "/tmp/test.txt",
				Mode:   0644,
			},
		},
	}); err != nil {
		t.Fatalf("Send WriteHeader: %v", err)
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false (placeholder)")
	}
	if resp.Error == "" {
		t.Error("expected placeholder error message")
	}
}

func TestOperations_Write_NoHeader(t *testing.T) {
	env := newFullTestEnv(t, nil)

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Write(ctx)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Send data instead of header.
	if err := stream.Send(&operationspb.WriteInput{
		Payload: &operationspb.WriteInput_Data{
			Data: []byte("hello"),
		},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	_, err = stream.CloseAndRecv()
	if err == nil {
		t.Fatal("expected error when first message is not WriteHeader")
	}
}

func TestOperations_Forward(t *testing.T) {
	env := newFullTestEnv(t, nil)
	nodeID := connectAgent(t, env, "fwd-node")

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Forward(ctx)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	// Send TunnelOpen.
	if err := stream.Send(&operationspb.TunnelData{
		ConnectionId: "conn-1",
		Open: &operationspb.TunnelOpen{
			NodeId:     nodeID,
			RemotePort: 8080,
		},
	}); err != nil {
		t.Fatalf("Send TunnelOpen: %v", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if !resp.Close {
		t.Error("expected close=true (placeholder)")
	}
}

func TestOperations_Forward_NoTunnelOpen(t *testing.T) {
	env := newFullTestEnv(t, nil)

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Forward(ctx)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	// Send data without TunnelOpen.
	if err := stream.Send(&operationspb.TunnelData{
		ConnectionId: "conn-1",
		Payload:      []byte("hello"),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error when first message has no TunnelOpen")
	}
}

func TestOperations_Unauthenticated(t *testing.T) {
	env := newFullTestEnv(t, nil)

	oc := operationspb.NewOperationsServiceClient(env.conn)

	// No auth header.
	stream, err := oc.Exec(context.Background(), &operationspb.ExecRequest{
		NodeId:  "any",
		Command: "ls",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for unauthenticated request")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("got code %v, want %v", st.Code(), codes.Unauthenticated)
	}
}
