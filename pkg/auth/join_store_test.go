package auth

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestJoinStore(t *testing.T) *JoinTokenStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := NewJoinTokenStoreFromDB(db)
	if err != nil {
		t.Fatalf("NewJoinTokenStoreFromDB: %v", err)
	}
	return s
}

func sampleEntry(id, hash string) *JoinTokenEntry {
	return &JoinTokenEntry{
		ID:        id,
		TokenHash: hash,
		CreatedAt: time.Now().Unix(),
		MaxUses:   0,
		ExpiresAt: 0,
	}
}

func TestJoinStore_Insert(t *testing.T) {
	s := newTestJoinStore(t)
	e := sampleEntry("abc12345", "hash1")
	if err := s.Insert(e); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	tokens, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("want 1 token, got %d", len(tokens))
	}
	if tokens[0].ID != "abc12345" {
		t.Errorf("want ID abc12345, got %s", tokens[0].ID)
	}
}

func TestJoinStore_Validate_Success(t *testing.T) {
	s := newTestJoinStore(t)
	e := sampleEntry("tok00001", "hash-valid")
	if err := s.Insert(e); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := s.Validate("hash-valid")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.ID != "tok00001" {
		t.Errorf("want ID tok00001, got %s", got.ID)
	}
	if got.Uses != 1 {
		t.Errorf("want Uses=1, got %d", got.Uses)
	}
}

func TestJoinStore_Validate_Revoked(t *testing.T) {
	s := newTestJoinStore(t)
	e := sampleEntry("tok00002", "hash-revoked")
	if err := s.Insert(e); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := s.Revoke("tok00002"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, err := s.Validate("hash-revoked")
	if err == nil {
		t.Fatal("expected error for revoked token, got nil")
	}
}

func TestJoinStore_Validate_Expired(t *testing.T) {
	s := newTestJoinStore(t)
	e := sampleEntry("tok00003", "hash-expired")
	e.ExpiresAt = time.Now().Add(-time.Hour).Unix() // expired 1h ago
	if err := s.Insert(e); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	_, err := s.Validate("hash-expired")
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestJoinStore_Validate_MaxUsesExceeded(t *testing.T) {
	s := newTestJoinStore(t)
	e := sampleEntry("tok00004", "hash-maxuses")
	e.MaxUses = 2
	if err := s.Insert(e); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// First two uses should succeed.
	if _, err := s.Validate("hash-maxuses"); err != nil {
		t.Fatalf("first Validate: %v", err)
	}
	if _, err := s.Validate("hash-maxuses"); err != nil {
		t.Fatalf("second Validate: %v", err)
	}

	// Third should fail.
	_, err := s.Validate("hash-maxuses")
	if err == nil {
		t.Fatal("expected error for max_uses exceeded, got nil")
	}
}

func TestJoinStore_Validate_NotFound(t *testing.T) {
	s := newTestJoinStore(t)
	_, err := s.Validate("nonexistent-hash")
	if err == nil {
		t.Fatal("expected error for nonexistent token, got nil")
	}
}

func TestJoinStore_List(t *testing.T) {
	s := newTestJoinStore(t)
	for i, id := range []string{"tok00010", "tok00011", "tok00012"} {
		e := sampleEntry(id, "hash-list-"+id)
		e.CreatedAt = time.Now().Unix() + int64(i)
		if err := s.Insert(e); err != nil {
			t.Fatalf("Insert %s: %v", id, err)
		}
	}

	tokens, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tokens) != 3 {
		t.Fatalf("want 3 tokens, got %d", len(tokens))
	}
}

func TestJoinStore_Revoke(t *testing.T) {
	s := newTestJoinStore(t)
	e := sampleEntry("tok00020", "hash-revoke-test")
	if err := s.Insert(e); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := s.Revoke("tok00020"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Revoking again should fail.
	if err := s.Revoke("tok00020"); err == nil {
		t.Fatal("expected error revoking already-revoked token, got nil")
	}
}

func TestJoinStore_CountActive(t *testing.T) {
	s := newTestJoinStore(t)

	// Insert 3 tokens: 1 active, 1 revoked, 1 expired.
	if err := s.Insert(sampleEntry("tok00030", "hash-active")); err != nil {
		t.Fatalf("Insert active: %v", err)
	}

	revoked := sampleEntry("tok00031", "hash-revoked2")
	if err := s.Insert(revoked); err != nil {
		t.Fatalf("Insert to-revoke: %v", err)
	}
	if err := s.Revoke("tok00031"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	expired := sampleEntry("tok00032", "hash-expired2")
	expired.ExpiresAt = time.Now().Add(-time.Minute).Unix()
	if err := s.Insert(expired); err != nil {
		t.Fatalf("Insert expired: %v", err)
	}

	n, err := s.CountActive()
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	if n != 1 {
		t.Errorf("want CountActive=1, got %d", n)
	}
}
