package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	controlpb "github.com/garysng/axon/gen/proto/control"
	operationspb "github.com/garysng/axon/gen/proto/operations"
)

// taskBridge manages pending CLI requests waiting for agent data plane connections.
type taskBridge struct {
	mu    sync.Mutex
	slots map[string]*bridgeSlot
}

// bridgeSlot holds the channels where CLI and Agent exchange data for one task.
type bridgeSlot struct {
	taskID   string
	taskType controlpb.TaskType

	// down carries data from Server → Agent (requests and CLI-to-Agent relay).
	down chan *operationspb.TaskDataDown
	// up carries data from Agent → Server (results and Agent-to-CLI relay).
	up chan *operationspb.TaskDataUp

	// attached is closed when the Agent opens its HandleTask stream.
	attached chan struct{}
	// done is closed when the task completes.
	done chan struct{}
}

func newTaskBridge() *taskBridge {
	return &taskBridge{slots: make(map[string]*bridgeSlot)}
}

// Create registers a new bridge slot for a task. The CLI handler calls this
// before sending TaskSignal to the Agent.
func (b *taskBridge) Create(taskID string, taskType controlpb.TaskType) *bridgeSlot {
	slot := &bridgeSlot{
		taskID:   taskID,
		taskType: taskType,
		down:     make(chan *operationspb.TaskDataDown, 16),
		up:       make(chan *operationspb.TaskDataUp, 16),
		attached: make(chan struct{}),
		done:     make(chan struct{}),
	}
	b.mu.Lock()
	b.slots[taskID] = slot
	b.mu.Unlock()
	return slot
}

// Attach is called by the AgentOpsService when the Agent connects with a task_id.
// It returns the slot and true if found, or nil and false if no matching task exists.
func (b *taskBridge) Attach(taskID string) (*bridgeSlot, bool) {
	b.mu.Lock()
	slot, ok := b.slots[taskID]
	b.mu.Unlock()
	if ok {
		select {
		case <-slot.attached:
			// Already attached (shouldn't happen, but be safe).
		default:
			close(slot.attached)
		}
	}
	return slot, ok
}

// Remove deletes a slot from the bridge. The CLI handler calls this in defer.
func (b *taskBridge) Remove(taskID string) {
	b.mu.Lock()
	if slot, ok := b.slots[taskID]; ok {
		select {
		case <-slot.done:
		default:
			close(slot.done)
		}
		delete(b.slots, taskID)
	}
	b.mu.Unlock()
}

// WaitAttach blocks until the Agent attaches to the slot, the context is
// cancelled, or the timeout expires.
func (b *taskBridge) WaitAttach(ctx context.Context, slot *bridgeSlot, timeout time.Duration) error {
	select {
	case <-slot.attached:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return fmt.Errorf("agent did not respond within %s", timeout)
	}
}
