package auth

import (
	"testing"
	"time"
)

func newTestTokenStore(t *testing.T) *TokenStore {
	t.Helper()
	store, err := NewTokenStore(":memory:")
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestTokenStore_InsertAndList(t *testing.T) {
	store := newTestTokenStore(t)

	entry := &TokenEntry{
		ID:        "jti-1",
		Kind:      "cli",
		UserID:    "alice",
		NodeIDs:   []string{"node-a", "node-b"},
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	}
	if err := store.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	entries, err := store.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 token, got %d", len(entries))
	}
	if entries[0].ID != "jti-1" {
		t.Errorf("expected ID jti-1, got %s", entries[0].ID)
	}
	if entries[0].UserID != "alice" {
		t.Errorf("expected UserID alice, got %s", entries[0].UserID)
	}
	if len(entries[0].NodeIDs) != 2 {
		t.Errorf("expected 2 NodeIDs, got %d", len(entries[0].NodeIDs))
	}
}

func TestTokenStore_ListByKind(t *testing.T) {
	store := newTestTokenStore(t)

	for _, e := range []*TokenEntry{
		{ID: "jti-cli", Kind: "cli", UserID: "alice", IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix()},
		{ID: "jti-agent", Kind: "agent", UserID: "", IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix()},
	} {
		if err := store.Insert(e); err != nil {
			t.Fatalf("Insert %s: %v", e.ID, err)
		}
	}

	cli, err := store.List("cli")
	if err != nil {
		t.Fatalf("List cli: %v", err)
	}
	if len(cli) != 1 || cli[0].ID != "jti-cli" {
		t.Errorf("expected 1 cli token, got %d", len(cli))
	}

	agent, err := store.List("agent")
	if err != nil {
		t.Fatalf("List agent: %v", err)
	}
	if len(agent) != 1 || agent[0].ID != "jti-agent" {
		t.Errorf("expected 1 agent token, got %d", len(agent))
	}
}

func TestTokenStore_Revoke(t *testing.T) {
	store := newTestTokenStore(t)

	entry := &TokenEntry{
		ID:        "jti-rev",
		Kind:      "cli",
		UserID:    "bob",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := store.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := store.Revoke("jti-rev", "admin"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Revoked tokens should not appear in List.
	entries, err := store.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 tokens after revoke, got %d", len(entries))
	}

	// LoadRevoked should return the revoked ID.
	revoked, err := store.LoadRevoked()
	if err != nil {
		t.Fatalf("LoadRevoked: %v", err)
	}
	if len(revoked) != 1 || revoked[0] != "jti-rev" {
		t.Errorf("expected [jti-rev], got %v", revoked)
	}
}

func TestTokenStore_RevokeAlreadyRevoked(t *testing.T) {
	store := newTestTokenStore(t)

	entry := &TokenEntry{
		ID:        "jti-double",
		Kind:      "cli",
		UserID:    "bob",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := store.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.Revoke("jti-double", "admin"); err != nil {
		t.Fatalf("first Revoke: %v", err)
	}
	if err := store.Revoke("jti-double", "admin"); err == nil {
		t.Error("expected error on double revoke, got nil")
	}
}

func TestTokenStore_RevokeNotFound(t *testing.T) {
	store := newTestTokenStore(t)

	if err := store.Revoke("nonexistent", "admin"); err == nil {
		t.Error("expected error revoking nonexistent token, got nil")
	}
}

func TestTokenStore_LoadRevokedExcludesExpired(t *testing.T) {
	store := newTestTokenStore(t)

	// Insert an already-expired revoked token.
	expired := &TokenEntry{
		ID:        "jti-expired",
		Kind:      "cli",
		UserID:    "old",
		IssuedAt:  time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(), // already expired
	}
	if err := store.Insert(expired); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.Revoke("jti-expired", "admin"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	revoked, err := store.LoadRevoked()
	if err != nil {
		t.Fatalf("LoadRevoked: %v", err)
	}
	if len(revoked) != 0 {
		t.Errorf("expected 0 revoked (expired excluded), got %v", revoked)
	}
}

func TestTokenChecker_IsRevoked(t *testing.T) {
	checker, err := NewTokenChecker(nil)
	if err != nil {
		t.Fatalf("NewTokenChecker: %v", err)
	}

	if checker.IsRevoked("some-jti") {
		t.Error("expected false for unknown JTI")
	}

	checker.MarkRevoked("some-jti")
	if !checker.IsRevoked("some-jti") {
		t.Error("expected true after MarkRevoked")
	}
}

func TestTokenChecker_PreloadFromStore(t *testing.T) {
	store := newTestTokenStore(t)

	entry := &TokenEntry{
		ID:        "jti-preload",
		Kind:      "cli",
		UserID:    "test",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if err := store.Insert(entry); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := store.Revoke("jti-preload", "admin"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	checker, err := NewTokenChecker(store)
	if err != nil {
		t.Fatalf("NewTokenChecker: %v", err)
	}

	if !checker.IsRevoked("jti-preload") {
		t.Error("expected jti-preload to be revoked after preload")
	}
}
