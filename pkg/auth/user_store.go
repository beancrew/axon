package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

const createUsersSchema = `
CREATE TABLE IF NOT EXISTS users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    node_ids      TEXT NOT NULL,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    disabled      INTEGER NOT NULL DEFAULT 0
);
`

// UserEntry holds the persisted record for a single CLI user.
type UserEntry struct {
	Username     string
	PasswordHash string   // bcrypt hash
	NodeIDs      []string // allowed node IDs; ["*"] grants access to all nodes
	CreatedAt    int64    // Unix timestamp
	UpdatedAt    int64    // Unix timestamp
	Disabled     bool
}

// UserStore persists CLI users to a SQLite database.
type UserStore struct {
	db     *sql.DB
	ownsDB bool
}

// NewUserStore opens (or creates) a SQLite database at dbPath and
// initialises the users schema. Use ":memory:" for in-process tests.
func NewUserStore(dbPath string) (*UserStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("auth: open user db %q: %w", dbPath, err)
	}
	// Enable WAL mode for better concurrent read/write performance (consistent with registry store).
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("auth: set WAL: %w", err)
	}
	if _, err := db.Exec(createUsersSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("auth: init user schema: %w", err)
	}
	return &UserStore{db: db, ownsDB: true}, nil
}

// NewUserStoreFromDB creates a UserStore using an existing *sql.DB and
// runs the schema migration. The caller owns the database; Close() is a no-op.
func NewUserStoreFromDB(db *sql.DB) (*UserStore, error) {
	if _, err := db.Exec(createUsersSchema); err != nil {
		return nil, fmt.Errorf("auth: init user schema: %w", err)
	}
	return &UserStore{db: db, ownsDB: false}, nil
}

// Close releases the database connection only if this store owns it.
// Stores created via NewUserStoreFromDB do not close the shared DB.
func (s *UserStore) Close() error {
	if s.ownsDB {
		return s.db.Close()
	}
	return nil
}

// Insert adds a new user. Returns an error if the username already exists.
func (s *UserStore) Insert(e *UserEntry) error {
	nodeIDsJSON, err := json.Marshal(e.NodeIDs)
	if err != nil {
		return fmt.Errorf("auth: marshal node_ids: %w", err)
	}
	now := time.Now().Unix()
	_, err = s.db.Exec(
		`INSERT INTO users (username, password_hash, node_ids, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		e.Username, e.PasswordHash, string(nodeIDsJSON), now, now,
	)
	if err != nil {
		return fmt.Errorf("auth: insert user: %w", err)
	}
	return nil
}

// InsertIfAbsent inserts the user only if the username does not already exist.
// Returns true if the user was inserted, false if already present.
func (s *UserStore) InsertIfAbsent(e *UserEntry) (bool, error) {
	nodeIDsJSON, err := json.Marshal(e.NodeIDs)
	if err != nil {
		return false, fmt.Errorf("auth: marshal node_ids: %w", err)
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO users (username, password_hash, node_ids, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		e.Username, e.PasswordHash, string(nodeIDsJSON), now, now,
	)
	if err != nil {
		return false, fmt.Errorf("auth: bootstrap user: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Get returns the UserEntry for the given username, or (nil, nil) if not found.
func (s *UserStore) Get(username string) (*UserEntry, error) {
	row := s.db.QueryRow(
		`SELECT username, password_hash, node_ids, created_at, updated_at, disabled FROM users WHERE username = ?`,
		username,
	)
	return scanUserRow(row)
}

// Update selectively updates a user's password and/or node IDs.
// Pass an empty passwordHash to leave the password unchanged.
// Pass nil nodeIDs to leave node access unchanged; pass a non-nil (even empty)
// slice to explicitly set node_ids.
func (s *UserStore) Update(username, passwordHash string, nodeIDs []string) error {
	now := time.Now().Unix()

	hasPassword := passwordHash != ""
	hasNodeIDs := nodeIDs != nil

	if !hasPassword && !hasNodeIDs {
		return fmt.Errorf("auth: update user: nothing to update")
	}

	var (
		res sql.Result
		err error
	)
	switch {
	case hasPassword && hasNodeIDs:
		nodeIDsJSON, _ := json.Marshal(nodeIDs)
		res, err = s.db.Exec(
			`UPDATE users SET password_hash = ?, node_ids = ?, updated_at = ? WHERE username = ?`,
			passwordHash, string(nodeIDsJSON), now, username,
		)
	case hasPassword:
		res, err = s.db.Exec(
			`UPDATE users SET password_hash = ?, updated_at = ? WHERE username = ?`,
			passwordHash, now, username,
		)
	case hasNodeIDs:
		nodeIDsJSON, _ := json.Marshal(nodeIDs)
		res, err = s.db.Exec(
			`UPDATE users SET node_ids = ?, updated_at = ? WHERE username = ?`,
			string(nodeIDsJSON), now, username,
		)
	}
	if err != nil {
		return fmt.Errorf("auth: update user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("auth: user %q not found", username)
	}
	return nil
}

// Delete removes a user from the store.
func (s *UserStore) Delete(username string) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE username = ?`, username)
	if err != nil {
		return fmt.Errorf("auth: delete user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("auth: user %q not found", username)
	}
	return nil
}

// List returns all users ordered by username.
func (s *UserStore) List() ([]*UserEntry, error) {
	rows, err := s.db.Query(
		`SELECT username, password_hash, node_ids, created_at, updated_at, disabled FROM users ORDER BY username`,
	)
	if err != nil {
		return nil, fmt.Errorf("auth: list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []*UserEntry
	for rows.Next() {
		var e UserEntry
		var nodeIDsJSON string
		var disabled int
		if err := rows.Scan(&e.Username, &e.PasswordHash, &nodeIDsJSON, &e.CreatedAt, &e.UpdatedAt, &disabled); err != nil {
			return nil, fmt.Errorf("auth: scan user: %w", err)
		}
		if err := json.Unmarshal([]byte(nodeIDsJSON), &e.NodeIDs); err != nil {
			e.NodeIDs = nil
		}
		e.Disabled = disabled != 0
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// scanUserRow scans a single *sql.Row into a UserEntry. Returns (nil, nil) when not found.
func scanUserRow(row *sql.Row) (*UserEntry, error) {
	var e UserEntry
	var nodeIDsJSON string
	var disabled int
	if err := row.Scan(&e.Username, &e.PasswordHash, &nodeIDsJSON, &e.CreatedAt, &e.UpdatedAt, &disabled); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("auth: scan user: %w", err)
	}
	if err := json.Unmarshal([]byte(nodeIDsJSON), &e.NodeIDs); err != nil {
		e.NodeIDs = nil
	}
	e.Disabled = disabled != 0
	return &e, nil
}
