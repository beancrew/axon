package audit

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS audit_log (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	user_id   TEXT    NOT NULL,
	node_id   TEXT    NOT NULL,
	operation TEXT    NOT NULL,
	detail    TEXT    NOT NULL,
	status    TEXT    NOT NULL,
	duration  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log (timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_user_id   ON audit_log (user_id);
CREATE INDEX IF NOT EXISTS idx_audit_node_id   ON audit_log (node_id);
`

// Store is a SQLite-backed audit log store.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) the SQLite database at dbPath and initialises
// the schema. Use ":memory:" for an in-process ephemeral database.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("audit: open db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("audit: migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Insert writes a single AuditEntry to the database.
func (s *Store) Insert(entry AuditEntry) error {
	_, err := s.db.Exec(
		`INSERT INTO audit_log (timestamp, user_id, node_id, operation, detail, status, duration)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.Timestamp.UnixNano(),
		entry.UserID,
		entry.NodeID,
		string(entry.Operation),
		entry.Detail,
		string(entry.Status),
		int64(entry.Duration),
	)
	if err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}

// Query retrieves audit entries matching opts.
func (s *Store) Query(opts QueryOptions) ([]AuditEntry, error) {
	conds := []string{}
	args := []any{}

	if opts.StartTime != nil {
		conds = append(conds, "timestamp >= ?")
		args = append(args, opts.StartTime.UnixNano())
	}
	if opts.EndTime != nil {
		conds = append(conds, "timestamp <= ?")
		args = append(args, opts.EndTime.UnixNano())
	}
	if opts.NodeID != "" {
		conds = append(conds, "node_id = ?")
		args = append(args, opts.NodeID)
	}
	if opts.UserID != "" {
		conds = append(conds, "user_id = ?")
		args = append(args, opts.UserID)
	}
	if opts.Operation != "" {
		conds = append(conds, "operation = ?")
		args = append(args, string(opts.Operation))
	}

	query := "SELECT id, timestamp, user_id, node_id, operation, detail, status, duration FROM audit_log"
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY timestamp ASC"

	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var tsNano int64
		var op, status string
		var dur int64
		if err := rows.Scan(&e.ID, &tsNano, &e.UserID, &e.NodeID, &op, &e.Detail, &status, &dur); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		e.Timestamp = time.Unix(0, tsNano)
		e.Operation = Operation(op)
		e.Status = Status(status)
		e.Duration = time.Duration(dur)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: rows: %w", err)
	}
	return entries, nil
}

// Log implements the Auditor interface by calling Insert and discarding any
// error (suitable for fire-and-forget use; use Insert directly when error
// handling is required).
func (s *Store) Log(entry AuditEntry) {
	_ = s.Insert(entry) //nolint:errcheck
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
