# Data Plane Bridge — Design Document

## Overview

This document describes the remaining work to make Phase 1 end-to-end functional:

1. **Server binary** (`cmd/axon-server/main.go`)
2. **Server data plane bridge** (replace placeholders in `internal/server/operations.go`)
3. **Agent data plane connector** (wire TaskHandler in `internal/agent`)

After these three tasks, `axon exec/read/write/forward` will work end-to-end:
CLI → Server → Agent → local execution → results streamed back.

---

## Task 1: Server Binary

**File:** `cmd/axon-server/main.go`

**Difficulty:** Low — pure wiring, all logic exists in `internal/server`.

### Requirements

- Parse server config from YAML file (path via `--config` flag, default `./config.yaml`)
- Support environment variable substitution in config values (e.g. `${AXON_JWT_KEY}`)
- Create `server.ServerConfig` from parsed config
- Call `server.NewServer(cfg).Start(ctx)`
- Signal handling: SIGINT/SIGTERM → graceful shutdown
- Commands: `axon-server start`, `axon-server version`
- Use cobra for CLI framework (consistent with axon and axon-agent)

### Config File Format

```yaml
listen: ":50051"

tls:
  cert: ""    # Optional, empty = insecure (dev mode)
  key: ""

auth:
  jwt_signing_key: "${AXON_JWT_KEY}"
  token_expiry: "24h"

users:
  - username: admin
    password_hash: "$2a$10$..."
    node_ids: ["*"]

heartbeat:
  interval: "10s"
  timeout: "30s"

audit:
  db_path: "./audit.db"
```

### Server Config Struct Mapping

```go
// Parsed config → server.ServerConfig
ServerConfig{
    ListenAddr:        cfg.Listen,           // ":50051"
    TLSCertPath:       cfg.TLS.Cert,         // optional
    TLSKeyPath:        cfg.TLS.Key,          // optional
    JWTSecret:         cfg.Auth.JWTSigningKey,// from env var
    HeartbeatInterval: cfg.Heartbeat.Interval,// 10s
    HeartbeatTimeout:  cfg.Heartbeat.Timeout, // 30s
    AuditDBPath:       cfg.Audit.DBPath,      // "./audit.db"
    Users:             parseUsers(cfg.Users), // []server.UserEntry
}
```

### Implementation Outline

```go
func main() {
    if err := rootCmd().Execute(); err != nil {
        os.Exit(1)
    }
}

func startCmd() *cobra.Command {
    var configPath string
    cmd := &cobra.Command{
        Use: "start",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg, err := loadServerConfig(configPath)
            // ...
            ctx, cancel := signal.NotifyContext(context.Background(),
                syscall.SIGINT, syscall.SIGTERM)
            defer cancel()
            return server.NewServer(cfg).Start(ctx)
        },
    }
    cmd.Flags().StringVar(&configPath, "config", "./config.yaml", "config file path")
    return cmd
}
```

### Tests

- Config parsing (valid, missing fields, env var substitution)
- Command help output
- Version command output

---

## Task 2: Server Data Plane Bridge

**File:** `internal/server/operations.go` (rewrite placeholder methods)

**Difficulty:** High — this is the core of the system.

### Problem

Currently, when CLI calls `Exec`, the server:
1. Routes to the correct agent (via Router) ✅
2. Sends `TaskSignal` via control stream ✅
3. Returns a placeholder error ❌

We need step 3 to become: **wait for Agent to connect back on a data plane stream, then bridge CLI ↔ Agent streams**.

### Architecture

```
CLI stream ──→ Server (bridge) ←── Agent data stream

     CLI                    Server                      Agent
      │                       │                           │
      │ ── ExecRequest ─────→ │                           │
      │                       │ ── TaskSignal{id} ──────→ │
      │                       │                           │
      │                       │ ←── AgentDataStream{id} ─ │
      │                       │     (agent connects back)  │
      │                       │                           │
      │ ←── ExecOutput ────── │ ←── ExecOutput ────────── │
      │ ←── ExecOutput ────── │ ←── ExecOutput ────────── │
      │ ←── ExecExit ──────── │ ←── ExecExit ──────────── │
```

### New Proto: Agent Data Plane Service

The agent needs a way to connect back to the server with a task_id. Add a new service to `operations.proto`:

```protobuf
// Agent calls this to open a data plane stream for a dispatched task.
// The server bridges this stream to the waiting CLI stream.
service AgentDataService {
    // Agent exec: agent sends ExecOutput, server relays to CLI.
    rpc AgentExec(AgentDataConnect) returns (stream AgentExecRequest);
    // ... or use a unified approach:

    // Unified: Agent connects with task_id, server returns the request,
    // then bidirectional data flows.
    rpc HandleTask(stream AgentTaskMessage) returns (stream ServerTaskMessage);
}
```

**However**, adding a new proto service adds complexity. A simpler approach leverages the existing proto:

### Recommended: Task Rendezvous via Internal Channel

No proto changes needed. The server uses an internal rendezvous mechanism:

```go
// taskBridge manages pending CLI requests waiting for Agent data plane connections.
type taskBridge struct {
    mu       sync.Mutex
    pending  map[string]*bridgeSlot // taskID → slot
}

// bridgeSlot holds the channel where the CLI handler waits for the Agent
// to deliver results.
type bridgeSlot struct {
    taskID   string
    taskType controlpb.TaskType
    ready    chan struct{}        // closed when Agent attaches
    
    // For exec: the CLI provides the request, Agent reads it and sends output.
    execReq  *operationspb.ExecRequest
    execOut  chan *operationspb.ExecOutput  // Agent → CLI
    
    // For read:
    readReq  *operationspb.ReadRequest
    readOut  chan *operationspb.ReadOutput
    
    // For write:
    writeIn  chan *operationspb.WriteInput  // CLI → Agent
    writeRes chan *operationspb.WriteResponse // Agent → CLI
    
    // For forward:
    fwdIn    chan *operationspb.TunnelData  // CLI → Agent
    fwdOut   chan *operationspb.TunnelData  // Agent → CLI
    
    done     chan struct{}        // closed when task completes
    err      error
}
```

### But... Agent needs a stream to Server

The agent's exec/read/write/forward handlers currently use function callbacks (`send func(...)`) rather than actual gRPC streams. The agent needs a way to **open a gRPC connection back to the server's data plane**.

**The cleanest approach: Agent opens OperationsService streams to the server.**

Wait — that creates a chicken-and-egg: OperationsService is what the CLI calls. The Agent can't call the same RPCs.

### Final Design: Agent-side gRPC Service

**Add a new `AgentOpsService` in `operations.proto`:**

```protobuf
// AgentOpsService is called by the Agent to fulfill dispatched tasks.
// Each RPC matches a CLI-facing RPC, but with reversed stream direction.
service AgentOpsService {
    // Agent calls this after receiving TaskSignal(TASK_EXEC).
    // Server sends the ExecRequest, Agent streams back ExecOutput.
    rpc FulfillExec(FulfillRequest) returns (stream FulfillExecInput);
    // No — this doesn't work cleanly with streaming either direction.
}
```

This gets complicated. Let me simplify.

### **Final Design: Bidirectional Task Stream**

Add one RPC to `operations.proto` that the Agent calls to handle any task:

```protobuf
// Agent calls this to handle a dispatched task. Bidirectional stream
// carries task-specific messages in both directions.
service AgentOpsService {
    rpc HandleTask(stream TaskDataUp) returns (stream TaskDataDown);
}

// Agent → Server (results flowing up)
message TaskDataUp {
    string task_id = 1;  // Set in first message to identify the task
    oneof payload {
        ExecOutput exec_output = 2;
        ReadOutput read_output = 3;
        WriteResponse write_response = 4;
        TunnelData tunnel_data = 5;
    }
}

// Server → Agent (requests flowing down)
message TaskDataDown {
    string task_id = 1;
    oneof payload {
        ExecRequest exec_request = 2;
        ReadRequest read_request = 3;
        WriteInput write_input = 4;
        TunnelData tunnel_data = 5;
    }
}
```

### Flow (Exec Example)

```
1. CLI calls OperationsService.Exec(ExecRequest) — blocks on server
2. Server creates bridgeSlot{taskID, execReq} in taskBridge
3. Server sends TaskSignal{taskID, TASK_EXEC} via control stream to Agent
4. Agent receives TaskSignal → opens AgentOpsService.HandleTask stream
5. Agent sends TaskDataUp{task_id: taskID} (first message = handshake)
6. Server matches task_id → finds bridgeSlot → sends TaskDataDown{exec_request}
7. Agent runs command, streams TaskDataUp{exec_output} messages
8. Server relays each exec_output to CLI's Exec stream
9. Agent sends final ExecOutput{ExecExit} → Server relays → CLI stream closes
```

### Flow (Write Example — CLI streams to Agent)

```
1. CLI calls OperationsService.Write(stream WriteInput) — sends WriteHeader
2. Server reads WriteHeader, routes, creates bridgeSlot
3. Server sends TaskSignal → Agent opens HandleTask stream
4. Server sends TaskDataDown{write_input: WriteHeader} to Agent
5. CLI sends more WriteInput{data} → Server relays as TaskDataDown{write_input}
6. Agent writes file, sends TaskDataUp{write_response}
7. Server relays WriteResponse to CLI
```

### Flow (Forward — Bidirectional)

```
1. CLI calls OperationsService.Forward(stream TunnelData) — sends TunnelOpen
2. Server reads TunnelOpen, routes, creates bridgeSlot
3. Server sends TaskSignal → Agent opens HandleTask stream
4. Server relays TunnelOpen as TaskDataDown{tunnel_data}
5. Bidirectional relay:
   - CLI TunnelData → Server → TaskDataDown{tunnel_data} → Agent
   - Agent TaskDataUp{tunnel_data} → Server → TunnelData → CLI
```

### Task Bridge Implementation

```go
// internal/server/bridge.go

type taskBridge struct {
    mu      sync.Mutex
    slots   map[string]*bridgeSlot
}

type bridgeSlot struct {
    taskID   string
    taskType controlpb.TaskType
    
    // Channels for data flow
    down     chan *TaskDataDown  // Server → Agent
    up       chan *TaskDataUp   // Agent → Server
    
    // Signaling
    attached chan struct{}       // closed when Agent connects
    done     chan struct{}       // closed when task completes
}

func (b *taskBridge) Create(taskID string, taskType controlpb.TaskType) *bridgeSlot {
    slot := &bridgeSlot{
        taskID:   taskID,
        taskType: taskType,
        down:     make(chan *TaskDataDown, 16),
        up:       make(chan *TaskDataUp, 16),
        attached: make(chan struct{}),
        done:     make(chan struct{}),
    }
    b.mu.Lock()
    b.slots[taskID] = slot
    b.mu.Unlock()
    return slot
}

func (b *taskBridge) Attach(taskID string) (*bridgeSlot, bool) {
    b.mu.Lock()
    slot, ok := b.slots[taskID]
    b.mu.Unlock()
    if ok {
        close(slot.attached)
    }
    return slot, ok
}

func (b *taskBridge) Remove(taskID string) {
    b.mu.Lock()
    delete(b.slots, taskID)
    b.mu.Unlock()
}
```

### Updated Operations Service (Exec Example)

```go
func (s *OperationsService) Exec(req *operationspb.ExecRequest, stream grpc.ServerStreamingServer[operationspb.ExecOutput]) error {
    ctx := stream.Context()
    
    entry, err := s.router.Route(ctx, req.GetNodeId())
    if err != nil {
        return err
    }
    
    taskID := uuid.NewString()
    slot := s.bridge.Create(taskID, controlpb.TaskType_TASK_EXEC)
    defer s.bridge.Remove(taskID)
    
    // Send task signal to agent.
    if err := s.control.SendTaskSignal(entry.NodeID, taskID, controlpb.TaskType_TASK_EXEC); err != nil {
        return status.Errorf(codes.Internal, "send task signal: %v", err)
    }
    
    // Wait for agent to attach (with timeout).
    select {
    case <-slot.attached:
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(30 * time.Second):
        return status.Error(codes.DeadlineExceeded, "agent did not respond")
    }
    
    // Send the request to the agent via bridge.
    slot.down <- &TaskDataDown{
        TaskId: taskID,
        Payload: &TaskDataDown_ExecRequest{ExecRequest: req},
    }
    
    // Relay agent output to CLI.
    for {
        select {
        case msg := <-slot.up:
            if out := msg.GetExecOutput(); out != nil {
                if err := stream.Send(out); err != nil {
                    return err
                }
                // Check if this is the exit message.
                if out.GetExit() != nil {
                    return nil
                }
            }
        case <-ctx.Done():
            return ctx.Err()
        case <-slot.done:
            return nil
        }
    }
}
```

### Timeout

- Agent attach timeout: 30 seconds (configurable)
- If agent doesn't open HandleTask within timeout, CLI gets DEADLINE_EXCEEDED
- Task data channels are buffered (16) to handle brief backpressure

---

## Task 3: Agent Data Plane Connector

**Files:**
- `internal/agent/agent.go` — wire TaskHandler
- `internal/agent/dispatcher.go` — new file, dispatches tasks to handlers via data stream

**Difficulty:** Medium.

### Requirements

When the Agent receives a `TaskSignal` via the control stream, it must:

1. Open a `AgentOpsService.HandleTask` bidi stream to the server
2. Send first message with `task_id` (handshake)
3. Receive the task request from server (e.g. `ExecRequest`)
4. Dispatch to the appropriate handler (`ExecHandler`, `FileIOHandler`, `ForwardHandler`)
5. Stream results back via the `HandleTask` stream

### Implementation

```go
// internal/agent/dispatcher.go

type Dispatcher struct {
    serverAddr string
    conn       *grpc.ClientConn
    execH      *ExecHandler
    fileH      *FileIOHandler
    fwdH       *ForwardHandler
}

func (d *Dispatcher) HandleTask(ctx context.Context, taskID string, taskType controlpb.TaskType) {
    // Open HandleTask stream to server.
    client := operationspb.NewAgentOpsServiceClient(d.conn)
    stream, err := client.HandleTask(ctx)
    if err != nil {
        log.Printf("dispatcher: open stream for task %s: %v", taskID, err)
        return
    }
    
    // Handshake: send task_id.
    stream.Send(&TaskDataUp{TaskId: taskID})
    
    // Receive request.
    msg, err := stream.Recv()
    if err != nil {
        log.Printf("dispatcher: recv request for task %s: %v", taskID, err)
        return
    }
    
    switch taskType {
    case controlpb.TaskType_TASK_EXEC:
        req := msg.GetExecRequest()
        d.execH.Handle(ctx, req, func(out *operationspb.ExecOutput) error {
            return stream.Send(&TaskDataUp{
                TaskId:  taskID,
                Payload: &TaskDataUp_ExecOutput{ExecOutput: out},
            })
        })
    
    case controlpb.TaskType_TASK_READ:
        req := msg.GetReadRequest()
        d.fileH.HandleRead(req, func(out *operationspb.ReadOutput) error {
            return stream.Send(&TaskDataUp{
                TaskId:  taskID,
                Payload: &TaskDataUp_ReadOutput{ReadOutput: out},
            })
        })
    
    case controlpb.TaskType_TASK_WRITE:
        // For write, we need to relay WriteInput from server to handler.
        // First message was the WriteHeader, subsequent messages are data.
        header := msg.GetWriteInput()
        d.fileH.HandleWrite(
            header,
            func() (*operationspb.WriteInput, error) {
                msg, err := stream.Recv()
                if err != nil { return nil, err }
                return msg.GetWriteInput(), nil
            },
            func(resp *operationspb.WriteResponse) error {
                return stream.Send(&TaskDataUp{
                    TaskId:  taskID,
                    Payload: &TaskDataUp_WriteResponse{WriteResponse: resp},
                })
            },
        )
    
    case controlpb.TaskType_TASK_FORWARD:
        open := msg.GetTunnelData()
        d.fwdH.Handle(ctx, open.GetOpen().GetRemotePort(), open.GetConnectionId(),
            func() (*operationspb.TunnelData, error) {
                msg, err := stream.Recv()
                if err != nil { return nil, err }
                return msg.GetTunnelData(), nil
            },
            func(td *operationspb.TunnelData) error {
                return stream.Send(&TaskDataUp{
                    TaskId:  taskID,
                    Payload: &TaskDataUp_TunnelData{TunnelData: td},
                })
            },
        )
    }
}
```

### Wiring in Agent

```go
// internal/agent/agent.go — in NewAgent or Run

dispatcher := &Dispatcher{
    serverAddr: cfg.ServerAddr,
    conn:       conn, // reuse the existing gRPC connection
    execH:      &ExecHandler{},
    fileH:      &FileIOHandler{},
    fwdH:       &ForwardHandler{},
}

agent.SetTaskHandler(func(taskID string, taskType controlpb.TaskType) {
    dispatcher.HandleTask(ctx, taskID, taskType)
})
```

### FileIOHandler.HandleWrite Refactor

Current `HandleWrite` signature uses gRPC streams directly. It needs to be refactored to accept generic `recv`/`send` callbacks (similar to how `ForwardHandler.Handle` already works).

---

## Proto Changes Summary

Add to `proto/operations.proto`:

```protobuf
// ─── Agent Data Plane ──────────────────────────────────────────────────────

// AgentOpsService is called by agents to fulfill dispatched tasks.
service AgentOpsService {
    // Bidirectional stream for task execution.
    // Agent sends task_id in first message, then streams results.
    // Server sends task request, then relays CLI data (for write/forward).
    rpc HandleTask(stream TaskDataUp) returns (stream TaskDataDown);
}

// Agent → Server
message TaskDataUp {
    string task_id = 1;
    oneof payload {
        ExecOutput exec_output = 2;
        ReadOutput read_output = 3;
        WriteResponse write_response = 4;
        TunnelData tunnel_data = 5;
    }
}

// Server → Agent
message TaskDataDown {
    string task_id = 1;
    oneof payload {
        ExecRequest exec_request = 2;
        ReadRequest read_request = 3;
        WriteInput write_input = 4;
        TunnelData tunnel_data = 5;
    }
}
```

---

## Implementation Order

```
Step 1: Proto changes
        Add TaskDataUp, TaskDataDown, AgentOpsService to operations.proto
        Run proto-gen

Step 2: Server bridge (internal/server/bridge.go)
        Implement taskBridge + bridgeSlot

Step 3: Server AgentOpsService (internal/server/agent_ops.go)
        Implement HandleTask — match task_id, relay data

Step 4: Server OperationsService rewrite (internal/server/operations.go)
        Replace placeholders with bridge-based implementation

Step 5: Agent dispatcher (internal/agent/dispatcher.go)
        Implement task dispatch + HandleTask client

Step 6: Agent wiring (internal/agent/agent.go + cmd/axon-agent/main.go)
        Wire dispatcher as TaskHandler

Step 7: Server binary (cmd/axon-server/main.go)
        Wire config parsing + server start

Step 8: Integration test
        End-to-end: start server + agent + CLI exec/read/write/forward
```

---

## Task Assignment

| Step | Task | Assignee | Depends On |
|------|------|----------|------------|
| 1 | Proto changes | Planner | — |
| 2 | Task bridge | Coder | Step 1 |
| 3 | AgentOpsService | Coder | Step 2 |
| 4 | Operations rewrite | Coder | Step 2, 3 |
| 5 | Agent dispatcher | Coder | Step 1 |
| 6 | Agent wiring | Coder | Step 5 |
| 7 | Server binary | Coder | Step 4 |
| 8 | Integration test | QA | Step 7 |

Steps 2-4 (server side) and Steps 5-6 (agent side) can be developed in parallel after Step 1.

---

## Risk & Mitigation

| Risk | Impact | Mitigation |
|------|--------|------------|
| Agent attach timeout (agent slow to respond) | CLI hangs | 30s timeout with clear error message |
| Channel deadlock (bridge full) | System hangs | Buffered channels (16) + select with ctx.Done |
| Agent crash mid-task | Orphaned CLI stream | Bridge detects stream close → error to CLI |
| Multiple agents same node_id | Routing confusion | Registry enforces one stream per node |
| Proto backward compatibility | Build break | New service, no changes to existing messages |
