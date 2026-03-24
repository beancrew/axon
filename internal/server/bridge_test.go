package server

import (
	"context"
	"testing"
	"time"

	controlpb "github.com/garysng/axon/gen/proto/control"
)

func TestBridge_CreateAttachRemove(t *testing.T) {
	b := newTaskBridge()

	slot := b.Create("task-1", controlpb.TaskType_TASK_EXEC)
	if slot == nil {
		t.Fatal("Create returned nil")
	}
	if slot.taskID != "task-1" {
		t.Errorf("taskID = %q, want %q", slot.taskID, "task-1")
	}

	// Attach should succeed and close the attached channel.
	got, ok := b.Attach("task-1")
	if !ok {
		t.Fatal("Attach returned false")
	}
	if got != slot {
		t.Fatal("Attach returned different slot")
	}

	// attached channel should be closed.
	select {
	case <-slot.attached:
	default:
		t.Error("attached channel not closed after Attach")
	}

	// Remove should close done and delete from map.
	b.Remove("task-1")
	select {
	case <-slot.done:
	default:
		t.Error("done channel not closed after Remove")
	}

	// Second Attach should fail.
	_, ok = b.Attach("task-1")
	if ok {
		t.Error("Attach succeeded after Remove")
	}
}

func TestBridge_WaitAttachTimeout(t *testing.T) {
	b := newTaskBridge()
	slot := b.Create("task-timeout", controlpb.TaskType_TASK_READ)

	err := b.WaitAttach(context.Background(), slot, 50*time.Millisecond)
	if err == nil {
		t.Fatal("WaitAttach should have timed out")
	}
}

func TestBridge_WaitAttachSuccess(t *testing.T) {
	b := newTaskBridge()
	slot := b.Create("task-ok", controlpb.TaskType_TASK_WRITE)

	// Attach in background.
	go func() {
		time.Sleep(10 * time.Millisecond)
		b.Attach("task-ok")
	}()

	err := b.WaitAttach(context.Background(), slot, 1*time.Second)
	if err != nil {
		t.Fatalf("WaitAttach: %v", err)
	}
}

func TestBridge_WaitAttachContextCancel(t *testing.T) {
	b := newTaskBridge()
	slot := b.Create("task-cancel", controlpb.TaskType_TASK_EXEC)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := b.WaitAttach(ctx, slot, 5*time.Second)
	if err == nil {
		t.Fatal("WaitAttach should have returned context error")
	}
}

func TestBridge_DoubleAttach(t *testing.T) {
	b := newTaskBridge()
	b.Create("task-double", controlpb.TaskType_TASK_FORWARD)

	_, ok := b.Attach("task-double")
	if !ok {
		t.Fatal("first Attach failed")
	}

	// Second Attach should still succeed (idempotent).
	_, ok = b.Attach("task-double")
	if !ok {
		t.Fatal("second Attach failed")
	}
}

func TestBridge_RemoveNonexistent(t *testing.T) {
	b := newTaskBridge()
	// Should not panic.
	b.Remove("no-such-task")
}

func TestBridge_DoubleRemove(t *testing.T) {
	b := newTaskBridge()
	b.Create("task-rm", controlpb.TaskType_TASK_EXEC)

	b.Remove("task-rm")
	// Should not panic.
	b.Remove("task-rm")
}
