package registry

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStore_SaveAndLoadAll(t *testing.T) {
	s := newTestStore(t)

	entry := &NodeEntry{
		NodeID:        "node-1",
		NodeName:      "web-1",
		TokenHash:     "sha256:abc123",
		Info:          NodeInfo{Arch: "arm64", IP: "10.0.0.1", AgentVersion: "0.1.0"},
		Status:        StatusOnline,
		ConnectedAt:   time.Now().Truncate(time.Second),
		LastHeartbeat: time.Now().Truncate(time.Second),
		RegisteredAt:  time.Now().Add(-time.Hour).Truncate(time.Second),
		Labels:        map[string]string{"env": "prod"},
	}

	if err := s.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}

	got := entries[0]
	if got.NodeID != "node-1" {
		t.Errorf("NodeID = %q", got.NodeID)
	}
	if got.NodeName != "web-1" {
		t.Errorf("NodeName = %q", got.NodeName)
	}
	if got.TokenHash != "sha256:abc123" {
		t.Errorf("TokenHash = %q", got.TokenHash)
	}
	if got.Info.Arch != "arm64" {
		t.Errorf("Arch = %q", got.Info.Arch)
	}
	if got.Labels["env"] != "prod" {
		t.Errorf("Labels = %v", got.Labels)
	}
}

func TestStore_Upsert(t *testing.T) {
	s := newTestStore(t)

	entry := &NodeEntry{
		NodeID:       "node-1",
		NodeName:     "web-1",
		TokenHash:    "hash",
		Status:       StatusOnline,
		RegisteredAt: time.Now(),
		Labels:       make(map[string]string),
	}
	if err := s.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Update.
	entry.Info.IP = "10.0.0.2"
	entry.Status = StatusOffline
	if err := s.Save(entry); err != nil {
		t.Fatalf("Save (upsert): %v", err)
	}

	entries, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
	if entries[0].Info.IP != "10.0.0.2" {
		t.Errorf("IP = %q after upsert", entries[0].Info.IP)
	}
	if entries[0].Status != StatusOffline {
		t.Errorf("Status = %q after upsert", entries[0].Status)
	}
}

func TestStore_Delete(t *testing.T) {
	s := newTestStore(t)

	entry := &NodeEntry{
		NodeID:       "node-del",
		NodeName:     "del-node",
		TokenHash:    "hash",
		Status:       StatusOnline,
		RegisteredAt: time.Now(),
		Labels:       make(map[string]string),
	}
	if err := s.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.Delete("node-del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	entries, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len = %d after delete, want 0", len(entries))
	}
}

func TestStore_UpdateHeartbeat(t *testing.T) {
	s := newTestStore(t)

	entry := &NodeEntry{
		NodeID:       "node-hb",
		NodeName:     "hb-node",
		TokenHash:    "hash",
		Status:       StatusOnline,
		RegisteredAt: time.Now(),
		Labels:       make(map[string]string),
	}
	if err := s.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	hbTime := time.Now().Add(5 * time.Minute).Truncate(time.Second)
	if err := s.UpdateHeartbeat("node-hb", hbTime); err != nil {
		t.Fatalf("UpdateHeartbeat: %v", err)
	}

	entries, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if entries[0].LastHeartbeat.Unix() != hbTime.Unix() {
		t.Errorf("LastHeartbeat = %v, want %v", entries[0].LastHeartbeat, hbTime)
	}
}

func TestStore_UpdateStatus(t *testing.T) {
	s := newTestStore(t)

	entry := &NodeEntry{
		NodeID:       "node-st",
		NodeName:     "st-node",
		TokenHash:    "hash",
		Status:       StatusOnline,
		RegisteredAt: time.Now(),
		Labels:       make(map[string]string),
	}
	if err := s.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.UpdateStatus("node-st", StatusOffline); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	entries, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if entries[0].Status != StatusOffline {
		t.Errorf("Status = %q, want %q", entries[0].Status, StatusOffline)
	}
}

func TestStore_LoadAll_Empty(t *testing.T) {
	s := newTestStore(t)

	entries, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len = %d, want 0", len(entries))
	}
}
