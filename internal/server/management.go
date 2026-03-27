// Package server provides the gRPC server and control plane implementation.
package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlpb "github.com/garysng/axon/gen/proto/control"
	managementpb "github.com/garysng/axon/gen/proto/management"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/auth"
)

// ManagementService implements managementpb.ManagementServiceServer.
type ManagementService struct {
	managementpb.UnimplementedManagementServiceServer
	reg               *registry.Registry
	secret            string
	tokenStore        *auth.TokenStore
	tokenChecker      *auth.TokenChecker
	joinTokenStore    *auth.JoinTokenStore
	tlsDir            string
	heartbeatInterval time.Duration
}

var _ managementpb.ManagementServiceServer = (*ManagementService)(nil)

func newManagementService(reg *registry.Registry, secret string, tokenStore *auth.TokenStore, tokenChecker *auth.TokenChecker, joinTokenStore *auth.JoinTokenStore, tlsDir string, heartbeatInterval time.Duration) *ManagementService {
	return &ManagementService{
		reg:               reg,
		secret:            secret,
		tokenStore:        tokenStore,
		tokenChecker:      tokenChecker,
		joinTokenStore:    joinTokenStore,
		tlsDir:            tlsDir,
		heartbeatInterval: heartbeatInterval,
	}
}

// ListNodes returns a summary of all nodes in the registry.
func (s *ManagementService) ListNodes(_ context.Context, _ *managementpb.ListNodesRequest) (*managementpb.ListNodesResponse, error) {
	entries := s.reg.List()
	nodes := make([]*managementpb.NodeSummary, 0, len(entries))
	for i := range entries {
		nodes = append(nodes, entryToSummary(&entries[i]))
	}
	return &managementpb.ListNodesResponse{Nodes: nodes}, nil
}

// GetNode returns detailed information about a node identified by ID or name.
func (s *ManagementService) GetNode(_ context.Context, req *managementpb.GetNodeRequest) (*managementpb.GetNodeResponse, error) {
	entry, ok := s.reg.Lookup(req.GetNodeId())
	if !ok {
		entry, ok = s.reg.LookupByName(req.GetNodeId())
		if !ok {
			return nil, status.Errorf(codes.NotFound, "management: node %q not found", req.GetNodeId())
		}
	}
	return &managementpb.GetNodeResponse{
		Summary:       entryToSummary(entry),
		UptimeSeconds: entry.Info.UptimeSeconds,
		Labels:        entry.Labels,
	}, nil
}

// RemoveNode removes a node from the registry.
func (s *ManagementService) RemoveNode(_ context.Context, req *managementpb.RemoveNodeRequest) (*managementpb.RemoveNodeResponse, error) {
	if err := s.reg.Remove(req.GetNodeId()); err != nil {
		return &managementpb.RemoveNodeResponse{Success: false, Error: err.Error()}, nil
	}
	return &managementpb.RemoveNodeResponse{Success: true}, nil
}

// Login is no longer supported; authentication is done via pre-issued tokens.
func (s *ManagementService) Login(_ context.Context, _ *managementpb.LoginRequest) (*managementpb.LoginResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Login is not supported; use a pre-issued token")
}

// CreateUser is no longer supported.
func (s *ManagementService) CreateUser(_ context.Context, _ *managementpb.CreateUserRequest) (*managementpb.CreateUserResponse, error) {
	return nil, status.Error(codes.Unimplemented, "user management is not supported")
}

// UpdateUser is no longer supported.
func (s *ManagementService) UpdateUser(_ context.Context, _ *managementpb.UpdateUserRequest) (*managementpb.UpdateUserResponse, error) {
	return nil, status.Error(codes.Unimplemented, "user management is not supported")
}

// DeleteUser is no longer supported.
func (s *ManagementService) DeleteUser(_ context.Context, _ *managementpb.DeleteUserRequest) (*managementpb.DeleteUserResponse, error) {
	return nil, status.Error(codes.Unimplemented, "user management is not supported")
}

// ListUsers is no longer supported.
func (s *ManagementService) ListUsers(_ context.Context, _ *managementpb.ListUsersRequest) (*managementpb.ListUsersResponse, error) {
	return nil, status.Error(codes.Unimplemented, "user management is not supported")
}

// entryToSummary converts a registry.NodeEntry to a managementpb.NodeSummary.
func entryToSummary(e *registry.NodeEntry) *managementpb.NodeSummary {
	return &managementpb.NodeSummary{
		NodeId:        e.NodeID,
		NodeName:      e.NodeName,
		Status:        e.Status,
		Arch:          e.Info.Arch,
		Ip:            e.Info.IP,
		AgentVersion:  e.Info.AgentVersion,
		ConnectedAt:   e.ConnectedAt.Unix(),
		LastHeartbeat: e.LastHeartbeat.Unix(),
		OsInfo:        osInfoToProto(&e.Info.OSInfo),
	}
}

// RevokeToken revokes a previously issued JWT by its JTI.
func (s *ManagementService) RevokeToken(ctx context.Context, req *managementpb.RevokeTokenRequest) (*managementpb.RevokeTokenResponse, error) {
	if s.tokenStore == nil {
		return &managementpb.RevokeTokenResponse{Error: "token management not enabled"}, nil
	}
	if req.GetTokenId() == "" {
		return &managementpb.RevokeTokenResponse{Error: "token_id is required"}, nil
	}

	claims, _ := auth.ClaimsFromContext(ctx)
	revokedBy := ""
	if claims != nil {
		revokedBy = claims.UserID
	}

	if err := s.tokenStore.Revoke(req.GetTokenId(), revokedBy); err != nil {
		return &managementpb.RevokeTokenResponse{Error: err.Error()}, nil
	}

	if s.tokenChecker != nil {
		s.tokenChecker.MarkRevoked(req.GetTokenId())
	}

	return &managementpb.RevokeTokenResponse{Success: true}, nil
}

// ListTokens returns active (non-revoked, non-expired) tokens.
func (s *ManagementService) ListTokens(_ context.Context, req *managementpb.ListTokensRequest) (*managementpb.ListTokensResponse, error) {
	if s.tokenStore == nil {
		return &managementpb.ListTokensResponse{}, nil
	}

	entries, err := s.tokenStore.List(req.GetKind())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "management: list tokens: %v", err)
	}

	tokens := make([]*managementpb.TokenInfo, 0, len(entries))
	for _, e := range entries {
		tokens = append(tokens, &managementpb.TokenInfo{
			Id:        e.ID,
			Kind:      e.Kind,
			UserId:    e.UserID,
			IssuedAt:  e.IssuedAt,
			ExpiresAt: e.ExpiresAt,
		})
	}

	return &managementpb.ListTokensResponse{Tokens: tokens}, nil
}

// JoinAgent validates a join token and issues an agent JWT + node ID.
// This method skips JWT auth in the interceptor chain (see server.go).
func (s *ManagementService) JoinAgent(_ context.Context, req *managementpb.JoinAgentRequest) (*managementpb.JoinAgentResponse, error) {
	if s.joinTokenStore == nil {
		return &managementpb.JoinAgentResponse{Error: "join token store not available"}, nil
	}
	if req.GetJoinToken() == "" {
		return &managementpb.JoinAgentResponse{Error: "join_token is required"}, nil
	}

	// Hash the plaintext join token with SHA-256 to look up the stored record.
	sum := sha256.Sum256([]byte(req.GetJoinToken()))
	tokenHash := hex.EncodeToString(sum[:])

	if _, err := s.joinTokenStore.Validate(tokenHash); err != nil {
		log.Printf("management: join token validation failed: %v", err)
		return &managementpb.JoinAgentResponse{Error: "invalid join token"}, nil
	}

	// Assign a stable node ID.
	nodeID := uuid.NewString()

	// Determine node name; fall back to node ID when not supplied.
	nodeName := req.GetNodeName()
	if nodeName == "" {
		nodeName = nodeID
	}

	// Register the node (offline — agent must Connect to come online).
	info := protoNodeInfoToRegistry(req.GetInfo())
	if err := s.reg.Register(nodeID, nodeName, "", info); err != nil {
		return &managementpb.JoinAgentResponse{Error: err.Error()}, nil
	}

	// Sign a long-lived agent JWT.
	const agentTokenExpiry = 0 // no expiry — tokens are revocable
	now := time.Now()
	tok, jti, err := auth.SignAgentToken(s.secret, nodeID, agentTokenExpiry)
	if err != nil {
		if removeErr := s.reg.Remove(nodeID); removeErr != nil {
			log.Printf("management: rollback register after sign failure: %v", removeErr)
		}
		return nil, status.Errorf(codes.Internal, "management: sign agent token: %v", err)
	}

	// Persist the issued token for listing and revocation.
	if s.tokenStore != nil {
		if err := s.tokenStore.Insert(&auth.TokenEntry{
			ID:        jti,
			Kind:      string(auth.KindAgent),
			NodeIDs:   []string{nodeID},
			IssuedAt:  now.Unix(),
			ExpiresAt: 0, // no expiry
		}); err != nil {
			log.Printf("management: persist agent token: %v", err)
		}
	}

	// Provide the CA cert PEM when auto-TLS is active.
	var caCertPEM string
	if s.tlsDir != "" {
		data, err := os.ReadFile(filepath.Join(s.tlsDir, "ca.crt"))
		if err != nil {
			log.Printf("management: read CA cert: %v", err)
		} else {
			caCertPEM = string(data)
		}
	}

	hbSeconds := int32(s.heartbeatInterval.Seconds())
	if hbSeconds == 0 {
		hbSeconds = 10
	}

	log.Printf("server: node %q (id=%s) enrolled via join-token", nodeName, nodeID)

	return &managementpb.JoinAgentResponse{
		Success:                  true,
		AgentToken:               tok,
		NodeId:                   nodeID,
		CaCertPem:                caCertPEM,
		HeartbeatIntervalSeconds: hbSeconds,
	}, nil
}

// CreateJoinToken generates a new join token and stores its hash.
func (s *ManagementService) CreateJoinToken(_ context.Context, req *managementpb.CreateJoinTokenRequest) (*managementpb.CreateJoinTokenResponse, error) {
	if s.joinTokenStore == nil {
		return nil, status.Errorf(codes.Unavailable, "management: join token store not available")
	}

	// Generate 32 random bytes → 64-char hex suffix (design: axon-join-<64-hex>).
	randBytes := make([]byte, 32)
	if _, err := rand.Read(randBytes); err != nil {
		return nil, status.Errorf(codes.Internal, "management: generate join token entropy: %v", err)
	}
	plaintext := "axon-join-" + hex.EncodeToString(randBytes)

	sum := sha256.Sum256([]byte(plaintext))
	tokenHash := hex.EncodeToString(sum[:])

	// Short 8-char hex ID for display/management.
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, status.Errorf(codes.Internal, "management: generate token ID: %v", err)
	}
	id := hex.EncodeToString(idBytes)
	now := time.Now()

	var expiresAt int64
	if req.GetExpiresSeconds() > 0 {
		expiresAt = now.Add(time.Duration(req.GetExpiresSeconds()) * time.Second).Unix()
	}

	if err := s.joinTokenStore.Insert(&auth.JoinTokenEntry{
		ID:        id,
		TokenHash: tokenHash,
		CreatedAt: now.Unix(),
		MaxUses:   int(req.GetMaxUses()),
		ExpiresAt: expiresAt,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "management: insert join token: %v", err)
	}

	return &managementpb.CreateJoinTokenResponse{
		Token: plaintext,
		Id:    id,
	}, nil
}

// ListJoinTokens returns all join tokens from the store.
func (s *ManagementService) ListJoinTokens(_ context.Context, _ *managementpb.ListJoinTokensRequest) (*managementpb.ListJoinTokensResponse, error) {
	if s.joinTokenStore == nil {
		return &managementpb.ListJoinTokensResponse{}, nil
	}
	entries, err := s.joinTokenStore.List()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "management: list join tokens: %v", err)
	}
	tokens := make([]*managementpb.JoinTokenInfo, 0, len(entries))
	for _, e := range entries {
		tokens = append(tokens, &managementpb.JoinTokenInfo{
			Id:        e.ID,
			CreatedAt: e.CreatedAt,
			Uses:      int32(e.Uses),
			MaxUses:   int32(e.MaxUses),
			ExpiresAt: e.ExpiresAt,
			Revoked:   e.Revoked,
		})
	}
	return &managementpb.ListJoinTokensResponse{Tokens: tokens}, nil
}

// RevokeJoinToken marks a join token as revoked by its ID.
func (s *ManagementService) RevokeJoinToken(_ context.Context, req *managementpb.RevokeJoinTokenRequest) (*managementpb.RevokeJoinTokenResponse, error) {
	if s.joinTokenStore == nil {
		return &managementpb.RevokeJoinTokenResponse{Error: "join token store not available"}, nil
	}
	if req.GetId() == "" {
		return &managementpb.RevokeJoinTokenResponse{Error: "id is required"}, nil
	}
	if err := s.joinTokenStore.Revoke(req.GetId()); err != nil {
		return &managementpb.RevokeJoinTokenResponse{Error: err.Error()}, nil
	}
	return &managementpb.RevokeJoinTokenResponse{Success: true}, nil
}

// osInfoToProto converts a registry.OSInfo to a controlpb.OSInfo.
func osInfoToProto(info *registry.OSInfo) *controlpb.OSInfo {
	if info == nil {
		return nil
	}
	return &controlpb.OSInfo{
		Os:              info.OS,
		OsVersion:       info.OSVersion,
		Platform:        info.Platform,
		PlatformVersion: info.PlatformVersion,
		PrettyName:      info.PrettyName,
	}
}
