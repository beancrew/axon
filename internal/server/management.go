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

// UserEntry holds credentials for a single CLI user.
type UserEntry struct {
	Username     string
	PasswordHash string   // bcrypt hash
	NodeIDs      []string // allowed node IDs; ["*"] grants access to all nodes
}

// ManagementService implements managementpb.ManagementServiceServer.
type ManagementService struct {
	managementpb.UnimplementedManagementServiceServer
	reg    *registry.Registry
	users  []UserEntry
	secret string
}

var _ managementpb.ManagementServiceServer = (*ManagementService)(nil)

func newManagementService(reg *registry.Registry, users []UserEntry, secret string) *ManagementService {
	return &ManagementService{reg: reg, users: users, secret: secret}
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
	for _, u := range s.users {
		if u.Username != req.GetUsername() {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.GetPassword())); err != nil {
			// Don't reveal whether the username was found.
			return &managementpb.LoginResponse{Error: "invalid credentials"}, nil
		}
		const tokenExpiry = 24 * time.Hour
		tok, err := auth.SignCLIToken(s.secret, u.Username, u.NodeIDs, tokenExpiry)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "management: sign token: %v", err)
		}
		return &managementpb.LoginResponse{
			Token:     tok,
			ExpiresAt: time.Now().Add(tokenExpiry).Unix(),
		}, nil
	}
	return &managementpb.LoginResponse{Error: "invalid credentials"}, nil
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
