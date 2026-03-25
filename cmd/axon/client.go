package main

import (
	"context"
	"fmt"

	managementpb "github.com/garysng/axon/gen/proto/management"
	operationspb "github.com/garysng/axon/gen/proto/operations"
	"github.com/garysng/axon/pkg/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// globalCACert and globalTLSInsecure are set by persistent root flags (--ca-cert,
// --tls-insecure) and override values read from the CLI config file.
var (
	globalCACert      string
	globalTLSInsecure bool
)

// dialConn creates a gRPC connection to the Axon server using CLI config.
// If withAuth is true, the connection includes the JWT token as bearer metadata.
// Returns the connection, a close function, and any error.
func dialConn(withAuth bool) (*grpc.ClientConn, func(), error) {
	cfg, err := config.LoadCLIConfig(config.DefaultCLIConfigPath())
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	if cfg.ServerAddr == "" {
		return nil, nil, fmt.Errorf("server address not configured; run: axon config set server <addr>")
	}

	// Flags override config-file values.
	if globalCACert != "" {
		cfg.CACert = globalCACert
	}
	if globalTLSInsecure {
		cfg.TLSInsecure = true
	}

	var transportOpt grpc.DialOption
	switch {
	case cfg.TLSInsecure:
		transportOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	case cfg.CACert != "":
		creds, err := credentials.NewClientTLSFromFile(cfg.CACert, "")
		if err != nil {
			return nil, nil, fmt.Errorf("load CA cert %q: %w", cfg.CACert, err)
		}
		transportOpt = grpc.WithTransportCredentials(creds)
	default:
		transportOpt = grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, ""))
	}

	opts := []grpc.DialOption{transportOpt}

	if withAuth {
		if cfg.Token == "" {
			return nil, nil, fmt.Errorf("not authenticated; run: axon auth login")
		}
		opts = append(opts,
			grpc.WithUnaryInterceptor(bearerUnaryInterceptor(cfg.Token)),
			grpc.WithStreamInterceptor(bearerStreamInterceptor(cfg.Token)),
		)
	}

	conn, err := grpc.NewClient(cfg.ServerAddr, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to server %q: %w", cfg.ServerAddr, err)
	}

	closer := func() { _ = conn.Close() }
	return conn, closer, nil
}

// dialServer creates a gRPC connection and returns a ManagementServiceClient.
func dialServer(withAuth bool) (managementpb.ManagementServiceClient, func(), error) {
	conn, closer, err := dialConn(withAuth)
	if err != nil {
		return nil, nil, err
	}
	return managementpb.NewManagementServiceClient(conn), closer, nil
}

// dialManagement creates an authenticated gRPC connection and returns a ManagementServiceClient.
func dialManagement() (managementpb.ManagementServiceClient, func(), error) {
	return dialServer(true)
}

// dialOperations creates a gRPC connection and returns an OperationsServiceClient.
func dialOperations() (operationspb.OperationsServiceClient, func(), error) {
	conn, closer, err := dialConn(true)
	if err != nil {
		return nil, nil, err
	}
	return operationspb.NewOperationsServiceClient(conn), closer, nil
}

// bearerUnaryInterceptor returns a unary client interceptor that attaches
// an "authorization: bearer <token>" metadata header to every unary RPC.
func bearerUnaryInterceptor(token string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "bearer "+token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// bearerStreamInterceptor returns a stream client interceptor that attaches
// an "authorization: bearer <token>" metadata header to every streaming RPC.
func bearerStreamInterceptor(token string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "bearer "+token)
		return streamer(ctx, desc, cc, method, opts...)
	}
}
