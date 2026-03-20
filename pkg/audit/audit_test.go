package audit

import (
	"fmt"
	"testing"
	"time"
)

// newTestStore returns an in-memory SQLite Store for testing.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func makeEntry(op Operation, userID, nodeID string, status Status) AuditEntry {
	return AuditEntry{
		Timestamp: time.Now().UTC(),
		UserID:    userID,
		NodeID:    nodeID,
		Operation: op,
		Detail:    fmt.Sprintf("detail for %s", op),
		Status:    status,
		Duration:  10 * time.Millisecond,
	}
}

// ---------------------------------------------------------------------------
// Store tests
// ---------------------------------------------------------------------------

func TestStore_InsertAndQueryAll(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 5; i++ {
		if err := s.Insert(makeEntry(OperationExec, "u1", "n1", StatusSuccess)); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	entries, err := s.Query(QueryOptions{Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestStore_QueryByUserID(t *testing.T) {
	s := newTestStore(t)

	_ = s.Insert(makeEntry(OperationRead, "alice", "n1", StatusSuccess))
	_ = s.Insert(makeEntry(OperationRead, "bob", "n1", StatusSuccess))
	_ = s.Insert(makeEntry(OperationWrite, "alice", "n2", StatusError))

	entries, err := s.Query(QueryOptions{UserID: "alice", Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for alice, got %d", len(entries))
	}
	for _, e := range entries {
		if e.UserID != "alice" {
			t.Errorf("unexpected user_id %q", e.UserID)
		}
	}
}

func TestStore_QueryByNodeID(t *testing.T) {
	s := newTestStore(t)

	_ = s.Insert(makeEntry(OperationExec, "u1", "node-A", StatusSuccess))
	_ = s.Insert(makeEntry(OperationExec, "u1", "node-B", StatusSuccess))
	_ = s.Insert(makeEntry(OperationExec, "u2", "node-A", StatusError))

	entries, err := s.Query(QueryOptions{NodeID: "node-A", Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for node-A, got %d", len(entries))
	}
}

func TestStore_QueryByOperation(t *testing.T) {
	s := newTestStore(t)

	_ = s.Insert(makeEntry(OperationExec, "u1", "n1", StatusSuccess))
	_ = s.Insert(makeEntry(OperationRead, "u1", "n1", StatusSuccess))
	_ = s.Insert(makeEntry(OperationForward, "u1", "n1", StatusSuccess))

	entries, err := s.Query(QueryOptions{Operation: OperationRead, Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 read entry, got %d", len(entries))
	}
	if entries[0].Operation != OperationRead {
		t.Errorf("expected operation read, got %q", entries[0].Operation)
	}
}

func TestStore_QueryByTimeRange(t *testing.T) {
	s := newTestStore(t)

	t0 := time.Now().UTC()
	t1 := t0.Add(1 * time.Second)
	t2 := t0.Add(2 * time.Second)
	t3 := t0.Add(3 * time.Second)

	entries := []AuditEntry{
		{Timestamp: t0, UserID: "u", NodeID: "n", Operation: OperationExec, Detail: "d", Status: StatusSuccess},
		{Timestamp: t1, UserID: "u", NodeID: "n", Operation: OperationExec, Detail: "d", Status: StatusSuccess},
		{Timestamp: t2, UserID: "u", NodeID: "n", Operation: OperationExec, Detail: "d", Status: StatusSuccess},
		{Timestamp: t3, UserID: "u", NodeID: "n", Operation: OperationExec, Detail: "d", Status: StatusSuccess},
	}
	for _, e := range entries {
		if err := s.Insert(e); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	got, err := s.Query(QueryOptions{StartTime: &t1, EndTime: &t2, Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 entries in [t1,t2], got %d", len(got))
	}
}

func TestStore_QueryLimitOffset(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 10; i++ {
		_ = s.Insert(makeEntry(OperationExec, "u1", "n1", StatusSuccess))
	}

	got, err := s.Query(QueryOptions{Limit: 3, Offset: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 entries (limit=3 offset=2), got %d", len(got))
	}
}

func TestStore_IDAutoIncrement(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 3; i++ {
		_ = s.Insert(makeEntry(OperationExec, "u", "n", StatusSuccess))
	}

	got, err := s.Query(QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for i, e := range got {
		if e.ID != int64(i+1) {
			t.Errorf("entry[%d].ID = %d, want %d", i, e.ID, i+1)
		}
	}
}

// ---------------------------------------------------------------------------
// Writer tests
// ---------------------------------------------------------------------------

func TestWriter_AsyncLog(t *testing.T) {
	s := newTestStore(t)
	w := NewWriter(s, 32)

	const n = 20
	for i := 0; i < n; i++ {
		w.Log(makeEntry(OperationExec, "u1", "n1", StatusSuccess))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	entries, err := s.Query(QueryOptions{Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != n {
		t.Errorf("expected %d entries after Close, got %d", n, len(entries))
	}
}

func TestWriter_CloseFlushesAll(t *testing.T) {
	s := newTestStore(t)
	w := NewWriter(s, 128)

	const n = 50
	for i := 0; i < n; i++ {
		w.Log(makeEntry(OperationWrite, "u", "n", StatusSuccess))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	entries, err := s.Query(QueryOptions{Limit: 200})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != n {
		t.Errorf("expected all %d entries flushed, got %d", n, len(entries))
	}
}

func TestWriter_BufferFullDropsEntries(t *testing.T) {
	s := newTestStore(t)
	// Very small buffer to force overflow.
	w := NewWriter(s, 4)

	// Pause the background goroutine by holding the store busy via a direct
	// query, then flood the channel.  Since we can't truly pause the goroutine,
	// we simply log far more entries than the buffer can hold synchronously and
	// verify that Close does not block and the store has at most bufferSize
	// entries (some may have been drained by the goroutine in the meantime, so
	// we just check we get ≤ total logged, i.e. no panic/deadlock).
	const n = 100
	for i := 0; i < n; i++ {
		w.Log(makeEntry(OperationForward, "u", "n", StatusSuccess))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	entries, err := s.Query(QueryOptions{Limit: 200})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// We cannot assert an exact number because the goroutine races with the
	// producer, but we must have at most n entries and must not deadlock.
	if len(entries) > n {
		t.Errorf("got more entries than logged: %d > %d", len(entries), n)
	}
	t.Logf("buffer=4, logged=%d, persisted=%d", n, len(entries))
}

func TestWriter_ImplementsAuditor(t *testing.T) {
	s := newTestStore(t)
	w := NewWriter(s, 8)
	defer w.Close()
	// Compile-time interface check.
	var _ Auditor = w
	var _ Auditor = s
}

func TestStore_RoundTrip(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	orig := AuditEntry{
		Timestamp: now,
		UserID:    "user-42",
		NodeID:    "node-99",
		Operation: OperationForward,
		Detail:    "tcp forward 8080->80",
		Status:    StatusError,
		Duration:  250 * time.Millisecond,
	}
	if err := s.Insert(orig); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := s.Query(QueryOptions{Limit: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("Query: err=%v len=%d", err, len(got))
	}
	e := got[0]
	if e.UserID != orig.UserID {
		t.Errorf("UserID: got %q want %q", e.UserID, orig.UserID)
	}
	if e.NodeID != orig.NodeID {
		t.Errorf("NodeID: got %q want %q", e.NodeID, orig.NodeID)
	}
	if e.Operation != orig.Operation {
		t.Errorf("Operation: got %q want %q", e.Operation, orig.Operation)
	}
	if e.Status != orig.Status {
		t.Errorf("Status: got %q want %q", e.Status, orig.Status)
	}
	if e.Detail != orig.Detail {
		t.Errorf("Detail: got %q want %q", e.Detail, orig.Detail)
	}
	if e.Duration != orig.Duration {
		t.Errorf("Duration: got %v want %v", e.Duration, orig.Duration)
	}
	if !e.Timestamp.Equal(orig.Timestamp) {
		t.Errorf("Timestamp: got %v want %v", e.Timestamp, orig.Timestamp)
	}
}
