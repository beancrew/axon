// Package server provides the gRPC server and control plane implementation.
package server

import (
	"context"
	"time"

	"golang.org/x/crypto/bcrypt"
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
	reg          *registry.Registry
	userStore    *auth.UserStore
	secret       string
	tokenStore   *auth.TokenStore
	tokenChecker *auth.TokenChecker
}

var _ managementpb.ManagementServiceServer = (*ManagementService)(nil)

func newManagementService(reg *registry.Registry, userStore *auth.UserStore, secret string, tokenStore *auth.TokenStore, tokenChecker *auth.TokenChecker) *ManagementService {
	return &ManagementService{
		reg:          reg,
		userStore:    userStore,
		secret:       secret,
		tokenStore:   tokenStore,
		tokenChecker: tokenChecker,
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

// Login validates credentials and issues a CLI JWT token.
// This method skips JWT auth in the interceptor chain (see server.go).
func (s *ManagementService) Login(_ context.Context, req *managementpb.LoginRequest) (*managementpb.LoginResponse, error) {
	if s.userStore == nil {
		return &managementpb.LoginResponse{Error: "user store not available"}, nil
	}
	u, err := s.userStore.Get(req.GetUsername())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "management: lookup user: %v", err)
	}
	if u == nil || u.Disabled {
		return &managementpb.LoginResponse{Error: "invalid credentials"}, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.GetPassword())); err != nil {
		// Don't reveal whether the username was found.
		return &managementpb.LoginResponse{Error: "invalid credentials"}, nil
	}
	const tokenExpiry = 24 * time.Hour
	now := time.Now()
	tok, jti, err := auth.SignCLIToken(s.secret, u.Username, u.NodeIDs, tokenExpiry)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "management: sign token: %v", err)
	}
	// Persist the issued token so it can be listed and revoked.
	if s.tokenStore != nil {
		_ = s.tokenStore.Insert(&auth.TokenEntry{
			ID:        jti,
			Kind:      string(auth.KindCLI),
			UserID:    u.Username,
			NodeIDs:   u.NodeIDs,
			IssuedAt:  now.Unix(),
			ExpiresAt: now.Add(tokenExpiry).Unix(),
		})
	}
	return &managementpb.LoginResponse{
		Token:     tok,
		ExpiresAt: now.Add(tokenExpiry).Unix(),
	}, nil
}

// CreateUser creates a new CLI user in the user store.
// TODO(P2-5): Add authorization check — only admin users should manage other users.
// TODO(P2-4): Log warning if TLS is not configured (password sent in plaintext).
func (s *ManagementService) CreateUser(_ context.Context, req *managementpb.CreateUserRequest) (*managementpb.CreateUserResponse, error) {
	if s.userStore == nil {
		return &managementpb.CreateUserResponse{Error: "user store not available"}, nil
	}
	if req.GetUsername() == "" {
		return &managementpb.CreateUserResponse{Error: "username is required"}, nil
	}
	if req.GetPassword() == "" {
		return &managementpb.CreateUserResponse{Error: "password is required"}, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.GetPassword()), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "management: hash password: %v", err)
	}
	nodeIDs := req.GetNodeIds()
	if len(nodeIDs) == 0 {
		nodeIDs = []string{"*"}
	}
	if err := s.userStore.Insert(&auth.UserEntry{
		Username:     req.GetUsername(),
		PasswordHash: string(hash),
		NodeIDs:      nodeIDs,
	}); err != nil {
		return &managementpb.CreateUserResponse{Error: err.Error()}, nil
	}
	return &managementpb.CreateUserResponse{Success: true}, nil
}

// UpdateUser updates an existing CLI user's password and/or node IDs.
// TODO(P2-5): Add authorization check — only admin users should manage other users.
// TODO(P2-4): Log warning if TLS is not configured (password sent in plaintext).
// TODO: Add disable/enable user support via a dedicated flag or RPC.
func (s *ManagementService) UpdateUser(_ context.Context, req *managementpb.UpdateUserRequest) (*managementpb.UpdateUserResponse, error) {
	if s.userStore == nil {
		return &managementpb.UpdateUserResponse{Error: "user store not available"}, nil
	}
	if req.GetUsername() == "" {
		return &managementpb.UpdateUserResponse{Error: "username is required"}, nil
	}
	var passwordHash string
	if req.GetPassword() != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.GetPassword()), bcrypt.DefaultCost)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "management: hash password: %v", err)
		}
		passwordHash = string(hash)
	}
	if err := s.userStore.Update(req.GetUsername(), passwordHash, req.GetNodeIds()); err != nil {
		return &managementpb.UpdateUserResponse{Error: err.Error()}, nil
	}
	return &managementpb.UpdateUserResponse{Success: true}, nil
}

// DeleteUser removes a CLI user from the user store.
func (s *ManagementService) DeleteUser(_ context.Context, req *managementpb.DeleteUserRequest) (*managementpb.DeleteUserResponse, error) {
	if s.userStore == nil {
		return &managementpb.DeleteUserResponse{Error: "user store not available"}, nil
	}
	if req.GetUsername() == "" {
		return &managementpb.DeleteUserResponse{Error: "username is required"}, nil
	}
	if err := s.userStore.Delete(req.GetUsername()); err != nil {
		return &managementpb.DeleteUserResponse{Error: err.Error()}, nil
	}
	return &managementpb.DeleteUserResponse{Success: true}, nil
}

// ListUsers returns all CLI users from the user store.
func (s *ManagementService) ListUsers(_ context.Context, _ *managementpb.ListUsersRequest) (*managementpb.ListUsersResponse, error) {
	if s.userStore == nil {
		return &managementpb.ListUsersResponse{}, nil
	}
	entries, err := s.userStore.List()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "management: list users: %v", err)
	}
	users := make([]*managementpb.UserInfo, 0, len(entries))
	for _, e := range entries {
		users = append(users, &managementpb.UserInfo{
			Username:  e.Username,
			NodeIds:   e.NodeIDs,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			Disabled:  e.Disabled,
		})
	}
	return &managementpb.ListUsersResponse{Users: users}, nil
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
