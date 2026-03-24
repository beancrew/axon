package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

const createTokensSchema = `
CREATE TABLE IF NOT EXISTS tokens (
    id          TEXT PRIMARY KEY,
    kind        TEXT NOT NULL,
    user_id     TEXT NOT NULL DEFAULT '',
    node_ids    TEXT NOT NULL DEFAULT '[]',
    issued_at   INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    revoked_at  INTEGER,
    revoked_by  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tokens_user ON tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_tokens_kind ON tokens(kind);
`

// TokenEntry holds the persisted record for a single issued JWT.
type TokenEntry struct {
	ID        string   // JWT ID (jti claim)
	Kind      string   // "cli" or "agent"
	UserID    string   // set for CLI tokens
	NodeIDs   []string // allowed nodes (CLI tokens)
	IssuedAt  int64    // Unix timestamp
	ExpiresAt int64    // Unix timestamp
	RevokedAt *int64   // nil if not revoked
	RevokedBy string   // username that revoked the token
}

// TokenStore persists issued tokens to a SQLite database.
type TokenStore struct {
	db *sql.DB
}

// NewTokenStore opens (or creates) a SQLite database at dbPath and
// initialises the tokens schema. Use ":memory:" for in-process tests.
func NewTokenStore(dbPath string) (*TokenStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("auth: open token db %q: %w", dbPath, err)
	}
	if _, err := db.Exec(createTokensSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("auth: init token schema: %w", err)
	}
	return &TokenStore{db: db}, nil
}

// Close releases the database connection.
func (s *TokenStore) Close() error {
	return s.db.Close()
}

// Insert records a newly-issued token.
func (s *TokenStore) Insert(e *TokenEntry) error {
	nodeIDsJSON, err := json.Marshal(e.NodeIDs)
	if err != nil {
		return fmt.Errorf("auth: marshal node_ids: %w", err)
	}
	_, err = s.db.Exec(
		`INSERT INTO tokens (id, kind, user_id, node_ids, issued_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.Kind, e.UserID, string(nodeIDsJSON), e.IssuedAt, e.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("auth: insert token: %w", err)
	}
	return nil
}

// Revoke marks a token as revoked. Returns an error if the token does not
// exist or was already revoked.
func (s *TokenStore) Revoke(id, revokedBy string) error {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE tokens SET revoked_at=?, revoked_by=? WHERE id=? AND revoked_at IS NULL`,
		now, revokedBy, id,
	)
	if err != nil {
		return fmt.Errorf("auth: revoke token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: revoke token rows: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("auth: token %q not found or already revoked", id)
	}
	return nil
}

// List returns active (non-revoked, non-expired) tokens. Pass kind="" to
// return tokens of all kinds; pass "cli" or "agent" to filter by kind.
func (s *TokenStore) List(kind string) ([]*TokenEntry, error) {
	now := time.Now().Unix()

	var (
		rows *sql.Rows
		err  error
	)
	if kind == "" {
		rows, err = s.db.Query(
			`SELECT id, kind, user_id, node_ids, issued_at, expires_at FROM tokens WHERE revoked_at IS NULL AND expires_at > ? ORDER BY issued_at DESC`,
			now,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, kind, user_id, node_ids, issued_at, expires_at FROM tokens WHERE revoked_at IS NULL AND expires_at > ? AND kind=? ORDER BY issued_at DESC`,
			now, kind,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("auth: list tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*TokenEntry
	for rows.Next() {
		var e TokenEntry
		var nodeIDsJSON string
		if err := rows.Scan(&e.ID, &e.Kind, &e.UserID, &nodeIDsJSON, &e.IssuedAt, &e.ExpiresAt); err != nil {
			return nil, fmt.Errorf("auth: scan token: %w", err)
		}
		if err := json.Unmarshal([]byte(nodeIDsJSON), &e.NodeIDs); err != nil {
			e.NodeIDs = nil
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// LoadRevoked returns revoked JTIs that have not yet expired from the database
// so the in-memory TokenChecker can be pre-populated on startup. Expired
// revoked tokens are excluded to avoid unbounded memory growth.
func (s *TokenStore) LoadRevoked() ([]string, error) {
	now := time.Now().Unix()
	rows, err := s.db.Query(`SELECT id FROM tokens WHERE revoked_at IS NOT NULL AND expires_at > ?`, now)
	if err != nil {
		return nil, fmt.Errorf("auth: load revoked tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("auth: scan revoked id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
