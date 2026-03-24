package registry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const nodeSchema = `
CREATE TABLE IF NOT EXISTS nodes (
    node_id         TEXT PRIMARY KEY,
    node_name       TEXT NOT NULL UNIQUE,
    token_hash      TEXT NOT NULL,
    arch            TEXT,
    ip              TEXT,
    agent_version   TEXT,
    os_info         TEXT,
    labels          TEXT,
    status          TEXT NOT NULL DEFAULT 'offline',
    connected_at    INTEGER,
    last_heartbeat  INTEGER,
    registered_at   INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_nodes_name   ON nodes(node_name);
CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status);
`

// Store defines the persistence interface for node entries.
type Store interface {
	LoadAll() ([]NodeEntry, error)
	Save(entry *NodeEntry) error
	Delete(nodeID string) error
	UpdateHeartbeat(nodeID string, t time.Time) error
	UpdateStatus(nodeID string, status string) error
	Close() error
}

// SQLiteStore persists node entries in a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the SQLite database at dbPath and
// initialises the nodes table. Use ":memory:" for testing.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("registry store: open db: %w", err)
	}
	// Enable WAL mode for better concurrent read/write performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("registry store: set WAL: %w", err)
	}
	if _, err := db.Exec(nodeSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("registry store: migrate schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

// LoadAll reads all node entries from the database.
func (s *SQLiteStore) LoadAll() ([]NodeEntry, error) {
	rows, err := s.db.Query(`
		SELECT node_id, node_name, token_hash, arch, ip, agent_version,
		       os_info, labels, status, connected_at, last_heartbeat,
		       registered_at, updated_at
		FROM nodes
	`)
	if err != nil {
		return nil, fmt.Errorf("registry store: load all: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []NodeEntry
	for rows.Next() {
		var (
			e                                       NodeEntry
			tokenHash, arch, ip, agentVer           string
			osInfoJSON, labelsJSON                   sql.NullString
			connAt, hbAt                            sql.NullInt64
			regAt, updAt                            int64
			statusStr                               string
		)
		if err := rows.Scan(
			&e.NodeID, &e.NodeName, &tokenHash, &arch, &ip, &agentVer,
			&osInfoJSON, &labelsJSON, &statusStr, &connAt, &hbAt,
			&regAt, &updAt,
		); err != nil {
			return nil, fmt.Errorf("registry store: scan: %w", err)
		}
		e.TokenHash = tokenHash
		e.Info.Arch = arch
		e.Info.IP = ip
		e.Info.AgentVersion = agentVer
		e.Status = statusStr
		e.RegisteredAt = time.Unix(regAt, 0)

		if connAt.Valid {
			e.ConnectedAt = time.Unix(connAt.Int64, 0)
		}
		if hbAt.Valid {
			e.LastHeartbeat = time.Unix(hbAt.Int64, 0)
		}
		if osInfoJSON.Valid && osInfoJSON.String != "" {
			_ = json.Unmarshal([]byte(osInfoJSON.String), &e.Info.OSInfo)
		}
		if labelsJSON.Valid && labelsJSON.String != "" {
			_ = json.Unmarshal([]byte(labelsJSON.String), &e.Labels)
		}
		if e.Labels == nil {
			e.Labels = make(map[string]string)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("registry store: rows: %w", err)
	}
	return entries, nil
}

// Save inserts or updates a node entry (UPSERT).
func (s *SQLiteStore) Save(entry *NodeEntry) error {
	osInfoJSON, _ := json.Marshal(entry.Info.OSInfo)
	labelsJSON, _ := json.Marshal(entry.Labels)
	now := time.Now().Unix()

	_, err := s.db.Exec(`
		INSERT INTO nodes (node_id, node_name, token_hash, arch, ip, agent_version,
		                   os_info, labels, status, connected_at, last_heartbeat,
		                   registered_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
		    node_name = excluded.node_name,
		    arch = excluded.arch,
		    ip = excluded.ip,
		    agent_version = excluded.agent_version,
		    os_info = excluded.os_info,
		    labels = excluded.labels,
		    status = excluded.status,
		    connected_at = excluded.connected_at,
		    last_heartbeat = excluded.last_heartbeat,
		    updated_at = excluded.updated_at
	`,
		entry.NodeID, entry.NodeName, entry.TokenHash,
		entry.Info.Arch, entry.Info.IP, entry.Info.AgentVersion,
		string(osInfoJSON), string(labelsJSON),
		entry.Status,
		nullableUnix(entry.ConnectedAt), nullableUnix(entry.LastHeartbeat),
		entry.RegisteredAt.Unix(), now,
	)
	if err != nil {
		return fmt.Errorf("registry store: save %s: %w", entry.NodeID, err)
	}
	return nil
}

// Delete removes a node entry from the database.
func (s *SQLiteStore) Delete(nodeID string) error {
	_, err := s.db.Exec("DELETE FROM nodes WHERE node_id = ?", nodeID)
	if err != nil {
		return fmt.Errorf("registry store: delete %s: %w", nodeID, err)
	}
	return nil
}

// UpdateHeartbeat updates the last_heartbeat timestamp for a node.
func (s *SQLiteStore) UpdateHeartbeat(nodeID string, t time.Time) error {
	_, err := s.db.Exec(
		"UPDATE nodes SET last_heartbeat = ?, updated_at = ? WHERE node_id = ?",
		t.Unix(), time.Now().Unix(), nodeID,
	)
	if err != nil {
		return fmt.Errorf("registry store: update heartbeat %s: %w", nodeID, err)
	}
	return nil
}

// UpdateStatus updates the status field for a node.
func (s *SQLiteStore) UpdateStatus(nodeID string, status string) error {
	_, err := s.db.Exec(
		"UPDATE nodes SET status = ?, updated_at = ? WHERE node_id = ?",
		status, time.Now().Unix(), nodeID,
	)
	if err != nil {
		return fmt.Errorf("registry store: update status %s: %w", nodeID, err)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// nullableUnix returns the Unix timestamp as *int64 if t is non-zero, or nil.
func nullableUnix(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	v := t.Unix()
	return &v
}
