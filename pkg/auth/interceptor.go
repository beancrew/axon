package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type contextKey struct{}

// ClaimsFromContext retrieves the Claims stored in ctx by the auth interceptors.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(contextKey{}).(*Claims)
	return c, ok && c != nil
}

// InjectClaims returns a child context carrying claims. This is intended for
// testing only; production code relies on the interceptors.
func InjectClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, contextKey{}, claims)
}

// UnaryInterceptor returns a gRPC unary server interceptor that validates the
// JWT supplied in the "authorization" metadata header and injects Claims into
// the request context. No revocation check is performed.
func UnaryInterceptor(secret string) grpc.UnaryServerInterceptor {
	return UnaryInterceptorWithChecker(secret, nil)
}

// StreamInterceptor returns a gRPC stream server interceptor that validates the
// JWT supplied in the "authorization" metadata header and injects Claims into
// the stream context. No revocation check is performed.
func StreamInterceptor(secret string) grpc.StreamServerInterceptor {
	return StreamInterceptorWithChecker(secret, nil)
}

// UnaryInterceptorWithChecker returns a gRPC unary server interceptor that
// validates the JWT and, when checker is non-nil, rejects tokens whose JTI
// appears in the revoked set.
func UnaryInterceptorWithChecker(secret string, checker *TokenChecker) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		newCtx, err := authenticateWithCheck(ctx, secret, checker)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
}

// StreamInterceptorWithChecker returns a gRPC stream server interceptor that
// validates the JWT and, when checker is non-nil, rejects tokens whose JTI
// appears in the revoked set.
func StreamInterceptorWithChecker(secret string, checker *TokenChecker) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		newCtx, err := authenticateWithCheck(ss.Context(), secret, checker)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ss, newCtx})
	}
}

// authenticateWithCheck validates the bearer token and optionally checks the
// revoked set.
func authenticateWithCheck(ctx context.Context, secret string, checker *TokenChecker) (context.Context, error) {
	newCtx, err := authenticate(ctx, secret)
	if err != nil {
		return nil, err
	}
	if checker != nil {
		if claims, ok := ClaimsFromContext(newCtx); ok && checker.IsRevoked(claims.ID) {
			return nil, status.Error(codes.Unauthenticated, "token has been revoked")
		}
	}
	return newCtx, nil
}

// authenticate extracts and validates the bearer token from gRPC metadata,
// returning a new context that carries the parsed Claims.
func authenticate(ctx context.Context, secret string) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	tokenStr := strings.TrimPrefix(values[0], "Bearer ")
	tokenStr = strings.TrimPrefix(tokenStr, "bearer ")

	claims, err := ValidateToken(secret, tokenStr)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}

	return context.WithValue(ctx, contextKey{}, claims), nil
}

// wrappedStream wraps a grpc.ServerStream so its context can be replaced.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}
