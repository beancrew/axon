package server

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationspb "github.com/garysng/axon/gen/proto/operations"
)

// TestOperations_Exec_NodeNotFound verifies that Exec returns NotFound for
// a non-existent node.
func TestOperations_Exec_NodeNotFound(t *testing.T) {
	env := newFullTestEnv(t)

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

// TestOperations_Exec_PermissionDenied verifies access control.
func TestOperations_Exec_PermissionDenied(t *testing.T) {
	env := newFullTestEnv(t)
	connectAgent(t, env, "restricted-node")

	oc := operationspb.NewOperationsServiceClient(env.conn)
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

// TestOperations_Write_NoHeader verifies that Write returns an error when
// the first message is not a WriteHeader.
func TestOperations_Write_NoHeader(t *testing.T) {
	env := newFullTestEnv(t)

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Write(ctx)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

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

// TestOperations_Forward_NoTunnelOpen verifies that Forward returns an error
// when the first message has no TunnelOpen.
func TestOperations_Forward_NoTunnelOpen(t *testing.T) {
	env := newFullTestEnv(t)

	oc := operationspb.NewOperationsServiceClient(env.conn)
	ctx := authedCtx(t, "admin", []string{"*"})

	stream, err := oc.Forward(ctx)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

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

// TestOperations_Unauthenticated verifies that requests without auth are rejected.
func TestOperations_Unauthenticated(t *testing.T) {
	env := newFullTestEnv(t)

	oc := operationspb.NewOperationsServiceClient(env.conn)

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
