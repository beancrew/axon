package auth

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testSecret = "super-secret-key-for-testing"

// ---- sign + verify round trips ----

func TestSignCLIToken_RoundTrip(t *testing.T) {
	nodeIDs := []string{"node-1", "node-2"}
	tok, err := SignCLIToken(testSecret, "user-42", nodeIDs, time.Hour)
	if err != nil {
		t.Fatalf("SignCLIToken error: %v", err)
	}

	claims, err := ValidateToken(testSecret, tok)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}
	if claims.UserID != "user-42" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-42")
	}
	if claims.Kind != kindCLI {
		t.Errorf("Kind = %q, want %q", claims.Kind, kindCLI)
	}
	if len(claims.NodeIDs) != 2 {
		t.Errorf("NodeIDs len = %d, want 2", len(claims.NodeIDs))
	}
}

func TestSignAgentToken_RoundTrip(t *testing.T) {
	tok, err := SignAgentToken(testSecret, "node-99", time.Hour)
	if err != nil {
		t.Fatalf("SignAgentToken error: %v", err)
	}

	claims, err := ValidateToken(testSecret, tok)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}
	if claims.NodeID != "node-99" {
		t.Errorf("NodeID = %q, want %q", claims.NodeID, "node-99")
	}
	if claims.Kind != kindAgent {
		t.Errorf("Kind = %q, want %q", claims.Kind, kindAgent)
	}
}

// ---- expired token ----

func TestValidateToken_Expired(t *testing.T) {
	tok, err := SignCLIToken(testSecret, "user-1", nil, -time.Second)
	if err != nil {
		t.Fatalf("SignCLIToken error: %v", err)
	}

	_, err = ValidateToken(testSecret, tok)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

// ---- invalid signature ----

func TestValidateToken_InvalidSignature(t *testing.T) {
	tok, err := SignCLIToken(testSecret, "user-1", nil, time.Hour)
	if err != nil {
		t.Fatalf("SignCLIToken error: %v", err)
	}

	_, err = ValidateToken("wrong-secret", tok)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestValidateToken_Malformed(t *testing.T) {
	_, err := ValidateToken(testSecret, "this.is.notavalidjwt")
	if err == nil {
		t.Fatal("expected error for malformed token, got nil")
	}
}

// ---- HasNodeAccess ----

func TestHasNodeAccess_CLIToken(t *testing.T) {
	tok, err := SignCLIToken(testSecret, "user-1", []string{"node-a", "node-b"}, time.Hour)
	if err != nil {
		t.Fatalf("SignCLIToken error: %v", err)
	}
	claims, err := ValidateToken(testSecret, tok)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}

	if !HasNodeAccess(claims, "node-a") {
		t.Error("expected access to node-a")
	}
	if !HasNodeAccess(claims, "node-b") {
		t.Error("expected access to node-b")
	}
	if HasNodeAccess(claims, "node-c") {
		t.Error("expected no access to node-c")
	}
}

func TestHasNodeAccess_AgentToken(t *testing.T) {
	tok, err := SignAgentToken(testSecret, "node-x", time.Hour)
	if err != nil {
		t.Fatalf("SignAgentToken error: %v", err)
	}
	claims, err := ValidateToken(testSecret, tok)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}

	if !HasNodeAccess(claims, "node-x") {
		t.Error("expected access to node-x")
	}
	if HasNodeAccess(claims, "node-y") {
		t.Error("expected no access to node-y")
	}
}

func TestHasNodeAccess_NilClaims(t *testing.T) {
	if HasNodeAccess(nil, "node-x") {
		t.Error("expected false for nil claims")
	}
}

// ---- ClaimsFromContext ----

func TestClaimsFromContext_Missing(t *testing.T) {
	_, ok := ClaimsFromContext(context.Background())
	if ok {
		t.Error("expected ok=false on empty context")
	}
}

func TestClaimsFromContext_Present(t *testing.T) {
	c := &Claims{UserID: "u1", Kind: kindCLI}
	ctx := context.WithValue(context.Background(), contextKey{}, c)
	got, ok := ClaimsFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "u1")
	}
}

// ---- gRPC UnaryInterceptor ----

func makeUnaryCtx(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewIncomingContext(context.Background(), md)
}

func dummyUnaryHandler(ctx context.Context, _ interface{}) (interface{}, error) {
	return ctx, nil
}

func TestUnaryInterceptor_ValidToken(t *testing.T) {
	tok, _ := SignCLIToken(testSecret, "u1", []string{"n1"}, time.Hour)
	ctx := makeUnaryCtx(tok)

	interceptor := UnaryInterceptor(testSecret)
	resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, dummyUnaryHandler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outCtx := resp.(context.Context)
	claims, ok := ClaimsFromContext(outCtx)
	if !ok {
		t.Fatal("expected claims in context")
	}
	if claims.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "u1")
	}
}

func TestUnaryInterceptor_MissingMetadata(t *testing.T) {
	interceptor := UnaryInterceptor(testSecret)
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, dummyUnaryHandler)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", code)
	}
}

func TestUnaryInterceptor_MissingAuthHeader(t *testing.T) {
	md := metadata.Pairs("x-other", "value")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	interceptor := UnaryInterceptor(testSecret)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, dummyUnaryHandler)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", code)
	}
}

func TestUnaryInterceptor_InvalidToken(t *testing.T) {
	ctx := makeUnaryCtx("invalid.jwt.token")

	interceptor := UnaryInterceptor(testSecret)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, dummyUnaryHandler)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", code)
	}
}

func TestUnaryInterceptor_ExpiredToken(t *testing.T) {
	tok, _ := SignCLIToken(testSecret, "u1", nil, -time.Second)
	ctx := makeUnaryCtx(tok)

	interceptor := UnaryInterceptor(testSecret)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, dummyUnaryHandler)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", code)
	}
}

// ---- gRPC StreamInterceptor ----

type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context { return m.ctx }

func TestStreamInterceptor_ValidToken(t *testing.T) {
	tok, _ := SignAgentToken(testSecret, "node-z", time.Hour)
	md := metadata.Pairs("authorization", "Bearer "+tok)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	stream := &mockServerStream{ctx: ctx}

	var capturedCtx context.Context
	handler := func(_ interface{}, ss grpc.ServerStream) error {
		capturedCtx = ss.Context()
		return nil
	}

	interceptor := StreamInterceptor(testSecret)
	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	claims, ok := ClaimsFromContext(capturedCtx)
	if !ok {
		t.Fatal("expected claims in stream context")
	}
	if claims.NodeID != "node-z" {
		t.Errorf("NodeID = %q, want %q", claims.NodeID, "node-z")
	}
}

func TestStreamInterceptor_InvalidToken(t *testing.T) {
	md := metadata.Pairs("authorization", "Bearer bad-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	stream := &mockServerStream{ctx: ctx}

	interceptor := StreamInterceptor(testSecret)
	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, func(_ interface{}, _ grpc.ServerStream) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", code)
	}
}
