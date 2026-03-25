package auth

import (
	"testing"
)

func newTestUserStore(t *testing.T) *UserStore {
	t.Helper()
	store, err := NewUserStore(":memory:")
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestUserStore_InsertAndGet(t *testing.T) {
	store := newTestUserStore(t)

	entry := &UserEntry{
		Username:     "alice",
		PasswordHash: "$2a$10$placeholder",
		NodeIDs:      []string{"*"},
	}
	if err := store.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := store.Get("alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.Username != "alice" {
		t.Errorf("expected Username=alice, got %s", got.Username)
	}
	if got.PasswordHash != "$2a$10$placeholder" {
		t.Errorf("unexpected PasswordHash: %s", got.PasswordHash)
	}
	if len(got.NodeIDs) != 1 || got.NodeIDs[0] != "*" {
		t.Errorf("expected NodeIDs=[*], got %v", got.NodeIDs)
	}
	if got.Disabled {
		t.Error("expected Disabled=false")
	}
	if got.CreatedAt == 0 {
		t.Error("expected CreatedAt to be set")
	}
}

func TestUserStore_GetNotFound(t *testing.T) {
	store := newTestUserStore(t)

	got, err := store.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing user, got %+v", got)
	}
}

func TestUserStore_InsertDuplicate(t *testing.T) {
	store := newTestUserStore(t)

	entry := &UserEntry{Username: "bob", PasswordHash: "hash", NodeIDs: []string{"node-1"}}
	if err := store.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.Insert(entry); err == nil {
		t.Error("expected error on duplicate insert, got nil")
	}
}

func TestUserStore_InsertIfAbsent(t *testing.T) {
	store := newTestUserStore(t)

	entry := &UserEntry{Username: "carol", PasswordHash: "hash", NodeIDs: []string{"*"}}

	inserted, err := store.InsertIfAbsent(entry)
	if err != nil {
		t.Fatalf("InsertIfAbsent (first): %v", err)
	}
	if !inserted {
		t.Error("expected inserted=true on first call")
	}

	inserted, err = store.InsertIfAbsent(entry)
	if err != nil {
		t.Fatalf("InsertIfAbsent (second): %v", err)
	}
	if inserted {
		t.Error("expected inserted=false when user already exists")
	}
}

func TestUserStore_List(t *testing.T) {
	store := newTestUserStore(t)

	for _, name := range []string{"charlie", "alice", "bob"} {
		if err := store.Insert(&UserEntry{Username: name, PasswordHash: "hash", NodeIDs: []string{"*"}}); err != nil {
			t.Fatalf("Insert %s: %v", name, err)
		}
	}

	entries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 users, got %d", len(entries))
	}
	// Should be alphabetically ordered.
	if entries[0].Username != "alice" || entries[1].Username != "bob" || entries[2].Username != "charlie" {
		t.Errorf("unexpected order: %v %v %v", entries[0].Username, entries[1].Username, entries[2].Username)
	}
}

func TestUserStore_Update(t *testing.T) {
	store := newTestUserStore(t)

	if err := store.Insert(&UserEntry{Username: "dave", PasswordHash: "old-hash", NodeIDs: []string{"node-1"}}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := store.Update("dave", "new-hash", []string{"node-1", "node-2"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get("dave")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.PasswordHash != "new-hash" {
		t.Errorf("expected PasswordHash=new-hash, got %s", got.PasswordHash)
	}
	if len(got.NodeIDs) != 2 {
		t.Errorf("expected 2 NodeIDs, got %v", got.NodeIDs)
	}
	if got.UpdatedAt < got.CreatedAt {
		t.Error("expected UpdatedAt >= CreatedAt")
	}
}

func TestUserStore_UpdatePasswordOnly(t *testing.T) {
	store := newTestUserStore(t)

	if err := store.Insert(&UserEntry{Username: "eve", PasswordHash: "hash-v1", NodeIDs: []string{"*"}}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Empty passwordHash means leave password unchanged, only update node_ids.
	if err := store.Update("eve", "", []string{"node-x"}); err != nil {
		t.Fatalf("Update (no password change): %v", err)
	}

	got, err := store.Get("eve")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PasswordHash != "hash-v1" {
		t.Errorf("expected PasswordHash unchanged, got %s", got.PasswordHash)
	}
	if len(got.NodeIDs) != 1 || got.NodeIDs[0] != "node-x" {
		t.Errorf("expected NodeIDs=[node-x], got %v", got.NodeIDs)
	}
}

func TestUserStore_UpdateNotFound(t *testing.T) {
	store := newTestUserStore(t)

	err := store.Update("ghost", "hash", []string{"*"})
	if err == nil {
		t.Error("expected error updating nonexistent user, got nil")
	}
}

func TestUserStore_Delete(t *testing.T) {
	store := newTestUserStore(t)

	if err := store.Insert(&UserEntry{Username: "frank", PasswordHash: "hash", NodeIDs: []string{"*"}}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := store.Delete("frank"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := store.Get("frank")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete, got user")
	}
}

func TestUserStore_DeleteNotFound(t *testing.T) {
	store := newTestUserStore(t)

	if err := store.Delete("nobody"); err == nil {
		t.Error("expected error deleting nonexistent user, got nil")
	}
}

func TestUserStore_NodeIDsRoundTrip(t *testing.T) {
	store := newTestUserStore(t)

	nodeIDs := []string{"web-1", "web-2", "db-primary"}
	if err := store.Insert(&UserEntry{Username: "grace", PasswordHash: "hash", NodeIDs: nodeIDs}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := store.Get("grace")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.NodeIDs) != 3 {
		t.Fatalf("expected 3 NodeIDs, got %d", len(got.NodeIDs))
	}
	for i, id := range nodeIDs {
		if got.NodeIDs[i] != id {
			t.Errorf("NodeIDs[%d]: expected %s, got %s", i, id, got.NodeIDs[i])
		}
	}
}

func TestUserStore_UpdatePasswordOnlyPreservesNodeIDs(t *testing.T) {
	store := newTestUserStore(t)

	if err := store.Insert(&UserEntry{Username: "hal", PasswordHash: "old-hash", NodeIDs: []string{"node-a", "node-b"}}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Update only password (nil nodeIDs = no change to node access).
	if err := store.Update("hal", "new-hash", nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := store.Get("hal")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PasswordHash != "new-hash" {
		t.Errorf("expected password new-hash, got %s", got.PasswordHash)
	}
	if len(got.NodeIDs) != 2 || got.NodeIDs[0] != "node-a" || got.NodeIDs[1] != "node-b" {
		t.Errorf("expected NodeIDs [node-a node-b] preserved, got %v", got.NodeIDs)
	}
}

func TestUserStore_UpdateNothingErrors(t *testing.T) {
	store := newTestUserStore(t)

	if err := store.Insert(&UserEntry{Username: "ivy", PasswordHash: "hash", NodeIDs: []string{"*"}}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Update with no password and nil nodeIDs should error.
	if err := store.Update("ivy", "", nil); err == nil {
		t.Error("expected error when updating nothing, got nil")
	}
}
