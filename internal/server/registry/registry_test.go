package registry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func sampleInfo() NodeInfo {
	return NodeInfo{
		Hostname:      "host-1",
		Arch:          "amd64",
		IP:            "10.0.0.1",
		UptimeSeconds: 3600,
		AgentVersion:  "0.1.0",
		OSInfo: OSInfo{
			OS:              "linux",
			OSVersion:       "6.8.0",
			Platform:        "ubuntu",
			PlatformVersion: "24.04",
			PrettyName:      "Ubuntu 24.04 LTS",
		},
	}
}

func TestRegisterAndLookup(t *testing.T) {
	r := NewRegistry(30 * time.Second)

	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	entry, ok := r.Lookup("node-1")
	if !ok {
		t.Fatal("expected to find node-1")
	}
	if entry.NodeID != "node-1" {
		t.Errorf("NodeID = %q, want %q", entry.NodeID, "node-1")
	}
	if entry.NodeName != "web-1" {
		t.Errorf("NodeName = %q, want %q", entry.NodeName, "web-1")
	}
	if entry.Status != StatusOnline {
		t.Errorf("Status = %q, want %q", entry.Status, StatusOnline)
	}
	if entry.Info.Hostname != "host-1" {
		t.Errorf("Info.Hostname = %q, want %q", entry.Info.Hostname, "host-1")
	}
}

func TestRegisterEmptyIDError(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.Register("", "web-1", sampleInfo()); err == nil {
		t.Fatal("expected error for empty nodeID")
	}
}

func TestLookupNotFound(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	_, ok := r.Lookup("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestLookupByName(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	entry, ok := r.LookupByName("web-1")
	if !ok {
		t.Fatal("expected to find node by name web-1")
	}
	if entry.NodeID != "node-1" {
		t.Errorf("NodeID = %q, want %q", entry.NodeID, "node-1")
	}
}

func TestLookupByNameNotFound(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	_, ok := r.LookupByName("unknown")
	if ok {
		t.Fatal("expected not found for unknown name")
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	before, _ := r.Lookup("node-1")
	time.Sleep(5 * time.Millisecond)

	if err := r.UpdateHeartbeat("node-1"); err != nil {
		t.Fatalf("UpdateHeartbeat: %v", err)
	}

	after, _ := r.Lookup("node-1")
	if !after.LastHeartbeat.After(before.LastHeartbeat) {
		t.Error("expected LastHeartbeat to be updated")
	}
}

func TestUpdateHeartbeatNotFound(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.UpdateHeartbeat("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestMarkOffline(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Set a stream so we can verify it gets cleared.
	if err := r.SetStream("node-1", "mock-stream"); err != nil {
		t.Fatalf("SetStream: %v", err)
	}

	if err := r.MarkOffline("node-1"); err != nil {
		t.Fatalf("MarkOffline: %v", err)
	}

	entry, _ := r.Lookup("node-1")
	if entry.Status != StatusOffline {
		t.Errorf("Status = %q, want %q", entry.Status, StatusOffline)
	}

	_, hasStream := r.GetStream("node-1")
	if hasStream {
		t.Error("expected stream to be cleared after MarkOffline")
	}
}

func TestMarkOfflineNotFound(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.MarkOffline("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestRemove(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := r.Remove("node-1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	_, ok := r.Lookup("node-1")
	if ok {
		t.Fatal("expected node-1 to be removed")
	}
}

func TestRemoveNotFound(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.Remove("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestList(t *testing.T) {
	r := NewRegistry(30 * time.Second)

	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("node-%d", i)
		name := fmt.Sprintf("web-%d", i)
		if err := r.Register(id, name, sampleInfo()); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}

	entries := r.List()
	if len(entries) != 3 {
		t.Errorf("List returned %d entries, want 3", len(entries))
	}
}

func TestListEmpty(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	entries := r.List()
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}
}

func TestHeartbeatTimeout(t *testing.T) {
	timeout := 50 * time.Millisecond
	r := NewRegistry(timeout)

	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.StartMonitor(ctx)

	// Wait long enough for the heartbeat to expire and the monitor to sweep.
	time.Sleep(timeout*3 + 10*time.Millisecond)

	entry, ok := r.Lookup("node-1")
	if !ok {
		t.Fatal("expected node-1 to still exist")
	}
	if entry.Status != StatusOffline {
		t.Errorf("Status = %q, want %q after heartbeat timeout", entry.Status, StatusOffline)
	}
}

func TestHeartbeatNotExpiredWhenUpdated(t *testing.T) {
	timeout := 100 * time.Millisecond
	r := NewRegistry(timeout)

	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.StartMonitor(ctx)

	// Keep heartbeating every 20ms — well within the 100ms timeout.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(timeout * 2)
		for {
			select {
			case <-ticker.C:
				_ = r.UpdateHeartbeat("node-1")
			case <-deadline:
				return
			}
		}
	}()
	<-done

	entry, _ := r.Lookup("node-1")
	if entry.Status != StatusOnline {
		t.Errorf("Status = %q, want %q — node should still be online", entry.Status, StatusOnline)
	}
}

func TestMonitorStopsOnContextCancel(t *testing.T) {
	timeout := 20 * time.Millisecond
	r := NewRegistry(timeout)

	ctx, cancel := context.WithCancel(context.Background())
	r.StartMonitor(ctx)

	// Cancel immediately; this should not panic or deadlock.
	cancel()
	time.Sleep(50 * time.Millisecond) // give goroutine time to exit cleanly
}

func TestSetAndGetStream(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Initially no stream.
	_, ok := r.GetStream("node-1")
	if ok {
		t.Error("expected no stream before SetStream")
	}

	mockStream := "mock-grpc-stream"
	if err := r.SetStream("node-1", mockStream); err != nil {
		t.Fatalf("SetStream: %v", err)
	}

	got, ok := r.GetStream("node-1")
	if !ok {
		t.Fatal("expected stream after SetStream")
	}
	if got != mockStream {
		t.Errorf("GetStream = %v, want %v", got, mockStream)
	}
}

func TestSetStreamNotFound(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.SetStream("nonexistent", "stream"); err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestGetStreamNotFound(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	_, ok := r.GetStream("nonexistent")
	if ok {
		t.Fatal("expected not found for nonexistent node")
	}
}

func TestReRegister(t *testing.T) {
	r := NewRegistry(30 * time.Second)
	if err := r.Register("node-1", "web-1", sampleInfo()); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.MarkOffline("node-1"); err != nil {
		t.Fatalf("MarkOffline: %v", err)
	}

	// Re-register same node — should come back online.
	newInfo := sampleInfo()
	newInfo.AgentVersion = "0.2.0"
	if err := r.Register("node-1", "web-1", newInfo); err != nil {
		t.Fatalf("second Register: %v", err)
	}

	entry, _ := r.Lookup("node-1")
	if entry.Status != StatusOnline {
		t.Errorf("Status = %q, want %q after re-register", entry.Status, StatusOnline)
	}
	if entry.Info.AgentVersion != "0.2.0" {
		t.Errorf("AgentVersion = %q, want %q", entry.Info.AgentVersion, "0.2.0")
	}
}

func TestConcurrentSafety(t *testing.T) {
	r := NewRegistry(30 * time.Second)

	const numNodes = 20
	const numWorkers = 10

	// Pre-register nodes.
	for i := 0; i < numNodes; i++ {
		id := fmt.Sprintf("node-%d", i)
		name := fmt.Sprintf("web-%d", i)
		if err := r.Register(id, name, sampleInfo()); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}

	var wg sync.WaitGroup

	// Concurrent heartbeat updates.
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < numNodes; i++ {
				id := fmt.Sprintf("node-%d", i)
				_ = r.UpdateHeartbeat(id)
			}
		}(w)
	}

	// Concurrent reads.
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < numNodes; i++ {
				id := fmt.Sprintf("node-%d", i)
				_, _ = r.Lookup(id)
				_ = r.List()
			}
		}(w)
	}

	// Concurrent stream sets.
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < numNodes; i++ {
				id := fmt.Sprintf("node-%d", i)
				_ = r.SetStream(id, "stream")
				_, _ = r.GetStream(id)
			}
		}(w)
	}

	wg.Wait()
}
