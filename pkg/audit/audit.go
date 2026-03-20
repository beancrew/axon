package audit

import "time"

// Operation represents the type of audited operation.
type Operation string

const (
	OperationExec    Operation = "exec"
	OperationRead    Operation = "read"
	OperationWrite   Operation = "write"
	OperationForward Operation = "forward"
)

// Status represents the outcome of an audited operation.
type Status string

const (
	StatusSuccess Status = "success"
	StatusError   Status = "error"
)

// AuditEntry holds a single audit log record.
type AuditEntry struct {
	ID        int64
	Timestamp time.Time
	UserID    string
	NodeID    string
	Operation Operation
	Detail    string
	Status    Status
	Duration  time.Duration
}

// QueryOptions filters audit log queries.
type QueryOptions struct {
	StartTime *time.Time
	EndTime   *time.Time
	NodeID    string
	UserID    string
	Operation Operation
	Limit     int
	Offset    int
}

// Auditor is the interface implemented by both Store and Writer.
type Auditor interface {
	Log(entry AuditEntry)
	Close() error
}
