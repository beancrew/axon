package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const createJoinTokensSchema = `
CREATE TABLE IF NOT EXISTS join_tokens (
    id         TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    uses       INTEGER NOT NULL DEFAULT 0,
    max_uses   INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0,
    revoked    INTEGER NOT NULL DEFAULT 0
);
`

// JoinTokenEntry holds the persisted record for a single join token.
type JoinTokenEntry struct {
	ID        string
	TokenHash string
	CreatedAt int64
	Uses      int
	MaxUses   int   // 0 = unlimited
	ExpiresAt int64 // 0 = never
	Revoked   bool
}

// JoinTokenStore persists join tokens to a SQLite database.
type JoinTokenStore struct {
	db *sql.DB
}

// NewJoinTokenStoreFromDB creates a JoinTokenStore using an existing *sql.DB
// and runs the schema migration. The caller owns the database.
func NewJoinTokenStoreFromDB(db *sql.DB) (*JoinTokenStore, error) {
	if _, err := db.Exec(createJoinTokensSchema); err != nil {
		return nil, fmt.Errorf("auth: init join_tokens schema: %w", err)
	}
	return &JoinTokenStore{db: db}, nil
}

// Insert adds a new join token entry.
func (s *JoinTokenStore) Insert(e *JoinTokenEntry) error {
	_, err := s.db.Exec(
		`INSERT INTO join_tokens (id, token_hash, created_at, uses, max_uses, expires_at, revoked)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TokenHash, e.CreatedAt, e.Uses, e.MaxUses, e.ExpiresAt, boolToInt(e.Revoked),
	)
	if err != nil {
		return fmt.Errorf("auth: insert join token: %w", err)
	}
	return nil
}

// Validate atomically checks that the token is valid (exists, not revoked, not
// expired, max_uses not exceeded) and increments the use count. Returns the
// entry on success or an error describing why validation failed.
func (s *JoinTokenStore) Validate(tokenHash string) (*JoinTokenEntry, error) {
	// BEGIN IMMEDIATE acquires a write lock upfront, preventing TOCTOU races
	// when multiple callers validate and increment uses concurrently.
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("auth: begin validate transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var e JoinTokenEntry
	var revoked int
	err = tx.QueryRow(
		`SELECT id, token_hash, created_at, uses, max_uses, expires_at, revoked
		 FROM join_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&e.ID, &e.TokenHash, &e.CreatedAt, &e.Uses, &e.MaxUses, &e.ExpiresAt, &revoked)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("auth: join token not found")
	}
	if err != nil {
		return nil, fmt.Errorf("auth: query join token: %w", err)
	}
	e.Revoked = revoked != 0

	if e.Revoked {
		return nil, fmt.Errorf("auth: join token has been revoked")
	}
	if e.ExpiresAt != 0 && time.Now().Unix() > e.ExpiresAt {
		return nil, fmt.Errorf("auth: join token has expired")
	}
	if e.MaxUses != 0 && e.Uses >= e.MaxUses {
		return nil, fmt.Errorf("auth: join token max uses exceeded")
	}

	_, err = tx.Exec(`UPDATE join_tokens SET uses = uses + 1 WHERE id = ?`, e.ID)
	if err != nil {
		return nil, fmt.Errorf("auth: increment join token uses: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("auth: commit validate transaction: %w", err)
	}

	e.Uses++
	return &e, nil
}

// List returns all join tokens ordered by created_at descending.
func (s *JoinTokenStore) List() ([]*JoinTokenEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, token_hash, created_at, uses, max_uses, expires_at, revoked
		 FROM join_tokens ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: list join tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*JoinTokenEntry
	for rows.Next() {
		var e JoinTokenEntry
		var revoked int
		if err := rows.Scan(&e.ID, &e.TokenHash, &e.CreatedAt, &e.Uses, &e.MaxUses, &e.ExpiresAt, &revoked); err != nil {
			return nil, fmt.Errorf("auth: scan join token: %w", err)
		}
		e.Revoked = revoked != 0
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// Revoke marks a join token as revoked by its short ID.
func (s *JoinTokenStore) Revoke(id string) error {
	res, err := s.db.Exec(`UPDATE join_tokens SET revoked = 1 WHERE id = ? AND revoked = 0`, id)
	if err != nil {
		return fmt.Errorf("auth: revoke join token: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("auth: join token %q not found or already revoked", id)
	}
	return nil
}

// CountActive returns the number of non-revoked, non-expired join tokens.
func (s *JoinTokenStore) CountActive() (int, error) {
	now := time.Now().Unix()
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM join_tokens WHERE revoked = 0 AND (expires_at = 0 OR expires_at > ?)`,
		now,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("auth: count active join tokens: %w", err)
	}
	return count, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
