package server

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlpb "github.com/garysng/axon/gen/proto/control"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/pkg/auth"
)

// ControlService implements controlpb.ControlServiceServer.
type ControlService struct {
	controlpb.UnimplementedControlServiceServer
	reg *registry.Registry
	cfg ServerConfig
}

var _ controlpb.ControlServiceServer = (*ControlService)(nil)

func newControlService(reg *registry.Registry, cfg ServerConfig) *ControlService {
	return &ControlService{reg: reg, cfg: cfg}
}

// Connect handles the bidirectional streaming control channel between the
// server and an agent. The protocol is:
//
//  1. First message must be a RegisterRequest with a valid agent token.
//  2. Subsequent messages may be Heartbeat or NodeInfo.
//  3. On disconnect the node is marked offline.
func (cs *ControlService) Connect(stream controlpb.ControlService_ConnectServer) error {
	// ── Step 1: expect RegisterRequest ────────────────────────────────────────
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "control: read first message: %v", err)
	}

	reg, ok := first.Payload.(*controlpb.AgentMessage_Register)
	if !ok || reg.Register == nil {
		return status.Error(codes.InvalidArgument, "control: first message must be RegisterRequest")
	}
	req := reg.Register

	// Validate agent token.
	claims, err := auth.ValidateToken(cs.cfg.JWTSecret, req.Token)
	if err != nil {
		_ = stream.Send(&controlpb.ServerMessage{
			Payload: &controlpb.ServerMessage_RegisterResponse{
				RegisterResponse: &controlpb.RegisterResponse{
					Success: false,
					Error:   "invalid token",
				},
			},
		})
		return status.Errorf(codes.Unauthenticated, "control: invalid agent token: %v", err)
	}
	_ = claims // claims available for future use (e.g. ACLs)

	tokenHash := hashToken(req.Token)
	info := protoNodeInfoToRegistry(req.Info)

	// Determine node ID: reconnection (node_id set) or first registration.
	var nodeID string
	if req.NodeId != "" {
		// Reconnection: verify the token hash matches the stored one.
		existing, ok := cs.reg.Lookup(req.NodeId)
		if !ok {
			return status.Errorf(codes.NotFound, "control: node %q not found for reconnection", req.NodeId)
		}
		if existing.TokenHash != "" && existing.TokenHash != tokenHash {
			return status.Errorf(codes.PermissionDenied, "control: token mismatch for node %q", req.NodeId)
		}
		nodeID = req.NodeId
	} else {
		// First registration: check if node_name already exists.
		if existing, ok := cs.reg.LookupByName(req.NodeName); ok {
			// Same name reconnecting — verify token hash only if online.
			// Offline nodes can be reclaimed with a new token.
			if existing.Status == "online" {
				return status.Errorf(codes.AlreadyExists, "control: node name %q already connected", req.NodeName)
			}
			if existing.TokenHash != "" && existing.TokenHash != tokenHash {
				return status.Errorf(codes.PermissionDenied,
					"control: node name %q is owned by a different token", req.NodeName)
			}
			nodeID = existing.NodeID
		} else {
			nodeID = uuid.NewString()
		}
	}

	if err := cs.reg.Register(nodeID, req.NodeName, tokenHash, info); err != nil {
		return status.Errorf(codes.Internal, "control: register node: %v", err)
	}

	// Persist the stream so task signals can be dispatched later.
	if err := cs.reg.SetStream(nodeID, stream); err != nil {
		return status.Errorf(codes.Internal, "control: set stream: %v", err)
	}

	// Mark node offline on disconnect (regardless of reason).
	defer func() {
		log.Printf("server: node %q (id=%s) disconnected", req.NodeName, nodeID)
		if err := cs.reg.MarkOffline(nodeID); err != nil {
			log.Printf("control: mark offline %s: %v", nodeID, err)
		}
	}()

	// Send RegisterResponse.
	intervalSec := int32(cs.cfg.HeartbeatInterval / time.Second)
	if intervalSec == 0 {
		intervalSec = 30
	}
	if err := stream.Send(&controlpb.ServerMessage{
		Payload: &controlpb.ServerMessage_RegisterResponse{
			RegisterResponse: &controlpb.RegisterResponse{
				Success:                  true,
				NodeId:                   nodeID,
				HeartbeatIntervalSeconds: intervalSec,
			},
		},
	}); err != nil {
		return status.Errorf(codes.Internal, "control: send register response: %v", err)
	}

	log.Printf("server: node %q (id=%s) connected", req.NodeName, nodeID)

	// ── Step 2: message loop ───────────────────────────────────────────────────
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			// Connection closed by client or network error.
			return nil //nolint:nilerr // disconnect is expected
		}

		switch p := msg.Payload.(type) {
		case *controlpb.AgentMessage_Heartbeat:
			_ = p
			if err := cs.reg.UpdateHeartbeat(nodeID); err != nil {
				log.Printf("control: update heartbeat %s: %v", nodeID, err)
			}
			if err := stream.Send(&controlpb.ServerMessage{
				Payload: &controlpb.ServerMessage_HeartbeatAck{
					HeartbeatAck: &controlpb.HeartbeatAck{
						ServerTimestamp: time.Now().UnixMilli(),
					},
				},
			}); err != nil {
				return nil // client gone
			}

		case *controlpb.AgentMessage_NodeInfo:
			if p.NodeInfo != nil {
				info := protoNodeInfoToRegistry(p.NodeInfo)
				// Re-register to update node info while preserving node ID.
				// UpdateInfo is not yet in the registry API, so we use Register
				// which updates fields on an existing entry.
				nodeName := ""
				if entry, ok := cs.reg.Lookup(nodeID); ok {
					nodeName = entry.NodeName
				}
				if err := cs.reg.Register(nodeID, nodeName, tokenHash, info); err != nil {
					log.Printf("control: update node info %s: %v", nodeID, err)
				}
				// Restore stream reference (Register clears it on re-register).
				if err := cs.reg.SetStream(nodeID, stream); err != nil {
					log.Printf("control: restore stream %s: %v", nodeID, err)
				}
			}

		default:
			// Unknown payload type; ignore.
		}
	}
}

// SendTaskSignal dispatches a TaskSignal to the agent identified by nodeID.
func (cs *ControlService) SendTaskSignal(nodeID, taskID string, taskType controlpb.TaskType) error {
	raw, ok := cs.reg.GetStream(nodeID)
	if !ok {
		return fmt.Errorf("control: send task signal: node %q has no active stream", nodeID)
	}
	stream, ok := raw.(controlpb.ControlService_ConnectServer)
	if !ok {
		return fmt.Errorf("control: send task signal: invalid stream type for node %q", nodeID)
	}
	return stream.Send(&controlpb.ServerMessage{
		Payload: &controlpb.ServerMessage_TaskSignal{
			TaskSignal: &controlpb.TaskSignal{
				TaskId: taskID,
				Type:   taskType,
			},
		},
	})
}

// protoNodeInfoToRegistry converts a protobuf NodeInfo to the registry type.
func protoNodeInfoToRegistry(info *controlpb.NodeInfo) registry.NodeInfo {
	if info == nil {
		return registry.NodeInfo{}
	}
	ri := registry.NodeInfo{
		Hostname:      info.Hostname,
		Arch:          info.Arch,
		IP:            info.Ip,
		UptimeSeconds: info.UptimeSeconds,
		AgentVersion:  info.AgentVersion,
	}
	if info.OsInfo != nil {
		ri.OSInfo = registry.OSInfo{
			OS:              info.OsInfo.Os,
			OSVersion:       info.OsInfo.OsVersion,
			Platform:        info.OsInfo.Platform,
			PlatformVersion: info.OsInfo.PlatformVersion,
			PrettyName:      info.OsInfo.PrettyName,
		}
	}
	return ri
}

// hashToken returns a hex-encoded SHA-256 hash of the given token string.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}
