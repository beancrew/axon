// Package registry provides an in-memory node registry with heartbeat monitoring.
package registry

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// OSInfo holds detailed operating system information for a node.
type OSInfo struct {
	OS              string // Kernel name: "linux", "darwin", "windows"
	OSVersion       string // Kernel version: "6.8.0-45-generic", "24.3.0"
	Platform        string // Distribution/platform: "ubuntu", "centos", "macOS"
	PlatformVersion string // Distribution version: "24.04", "9", "14.4"
	PrettyName      string // Human-readable: "Ubuntu 24.04 LTS", "macOS 14.4 Sonoma"
}

// NodeInfo holds hardware and OS information reported by an agent.
type NodeInfo struct {
	Hostname     string
	Arch         string // e.g. "amd64", "arm64"
	IP           string // Primary IP
	UptimeSeconds int64
	AgentVersion  string
	OSInfo       OSInfo
}

// NodeEntry represents a registered node in the registry.
type NodeEntry struct {
	NodeID        string
	NodeName      string
	TokenHash     string // SHA-256 of the agent token used at first registration
	Info          NodeInfo
	Status        string // "online" | "offline"
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	RegisteredAt  time.Time
	Labels        map[string]string
	stream        interface{} // control stream reference, protected by Registry mutex
}

const (
	StatusOnline  = "online"
	StatusOffline = "offline"
)

// Registry is a thread-safe in-memory store of node entries with optional
// persistent backing via a Store.
type Registry struct {
	mu               sync.RWMutex
	nodes            map[string]*NodeEntry // keyed by NodeID
	heartbeatTimeout time.Duration
	store            Store              // optional persistence backend (nil = in-memory only)
	hbBatcher        *heartbeatBatcher  // batched heartbeat persistence (nil if no store)
}

// NewRegistry creates a new Registry with the given heartbeat timeout.
// If store is non-nil, all nodes are loaded from the store on startup
// (with status set to offline until agents reconnect).
func NewRegistry(heartbeatTimeout time.Duration, store ...Store) *Registry {
	reg := &Registry{
		nodes:            make(map[string]*NodeEntry),
		heartbeatTimeout: heartbeatTimeout,
	}
	if len(store) > 0 && store[0] != nil {
		reg.store = store[0]
		entries, err := reg.store.LoadAll()
		if err != nil {
			log.Printf("registry: load from store: %v", err)
		} else {
			for i := range entries {
				entries[i].Status = StatusOffline
				cp := entries[i]
				reg.nodes[cp.NodeID] = &cp
			}
			if len(entries) > 0 {
				log.Printf("registry: loaded %d nodes from store", len(entries))
			}
		}
		reg.hbBatcher = newHeartbeatBatcher(reg.store, defaultFlushInterval)
		reg.hbBatcher.Start()
	}
	return reg
}

// Register adds or updates a node entry, setting its status to online.
// tokenHash is the SHA-256 of the agent token (used for stable identity verification).
func (r *Registry) Register(nodeID, nodeName string, info NodeInfo, tokenHash ...string) error {
	if nodeID == "" {
		return fmt.Errorf("registry: register: nodeID must not be empty")
	}
	now := time.Now()
	th := ""
	if len(tokenHash) > 0 {
		th = tokenHash[0]
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.nodes[nodeID]; ok {
		// Re-registration: update fields but preserve RegisteredAt and TokenHash.
		existing.NodeName = nodeName
		existing.Info = info
		existing.Status = StatusOnline
		existing.ConnectedAt = now
		existing.LastHeartbeat = now
		existing.stream = nil
		if r.store != nil {
			if err := r.store.Save(existing); err != nil {
				log.Printf("registry: persist re-register %s: %v", nodeID, err)
			}
		}
		return nil
	}

	entry := &NodeEntry{
		NodeID:        nodeID,
		NodeName:      nodeName,
		TokenHash:     th,
		Info:          info,
		Status:        StatusOnline,
		ConnectedAt:   now,
		LastHeartbeat: now,
		RegisteredAt:  now,
		Labels:        make(map[string]string),
	}
	r.nodes[nodeID] = entry

	if r.store != nil {
		if err := r.store.Save(entry); err != nil {
			log.Printf("registry: persist register %s: %v", nodeID, err)
		}
	}
	return nil
}

// UpdateHeartbeat updates the LastHeartbeat timestamp for the given node.
func (r *Registry) UpdateHeartbeat(nodeID string) error {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.nodes[nodeID]
	if !ok {
		return fmt.Errorf("registry: update heartbeat: node %q not found", nodeID)
	}
	entry.LastHeartbeat = now
	if r.hbBatcher != nil {
		r.hbBatcher.Record(nodeID, now)
	}
	return nil
}

// MarkOffline sets the node's status to offline and clears its control stream.
func (r *Registry) MarkOffline(nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.nodes[nodeID]
	if !ok {
		return fmt.Errorf("registry: mark offline: node %q not found", nodeID)
	}
	entry.Status = StatusOffline
	entry.stream = nil
	if r.store != nil {
		if err := r.store.UpdateStatus(nodeID, StatusOffline); err != nil {
			log.Printf("registry: persist mark offline %s: %v", nodeID, err)
		}
	}
	return nil
}

// Remove deletes a node entry entirely.
func (r *Registry) Remove(nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.nodes[nodeID]; !ok {
		return fmt.Errorf("registry: remove: node %q not found", nodeID)
	}
	delete(r.nodes, nodeID)
	if r.store != nil {
		if err := r.store.Delete(nodeID); err != nil {
			log.Printf("registry: persist remove %s: %v", nodeID, err)
		}
	}
	return nil
}

// List returns a snapshot of all node entries.
func (r *Registry) List() []NodeEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]NodeEntry, 0, len(r.nodes))
	for _, e := range r.nodes {
		out = append(out, *e)
	}
	return out
}

// Lookup returns a copy of the node entry for the given ID, if it exists.
func (r *Registry) Lookup(nodeID string) (*NodeEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.nodes[nodeID]
	if !ok {
		return nil, false
	}
	cp := *entry
	return &cp, true
}

// LookupByName returns a copy of the first node entry matching the given name.
func (r *Registry) LookupByName(nodeName string) (*NodeEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.nodes {
		if e.NodeName == nodeName {
			cp := *e
			return &cp, true
		}
	}
	return nil, false
}

// SetStream associates a control stream with the given node.
func (r *Registry) SetStream(nodeID string, stream interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.nodes[nodeID]
	if !ok {
		return fmt.Errorf("registry: set stream: node %q not found", nodeID)
	}
	entry.stream = stream
	return nil
}

// GetStream returns the control stream associated with the given node.
func (r *Registry) GetStream(nodeID string) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.nodes[nodeID]
	if !ok {
		return nil, false
	}
	return entry.stream, entry.stream != nil
}

// StartMonitor starts a background goroutine that periodically marks nodes
// as offline when their LastHeartbeat exceeds the heartbeatTimeout. It stops
// when ctx is cancelled.
func (r *Registry) StartMonitor(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.heartbeatTimeout / 2)
		if r.heartbeatTimeout < 2*time.Millisecond {
			// For very short timeouts (tests), poll more frequently.
			ticker = time.NewTicker(r.heartbeatTimeout / 2)
		}
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.sweepExpired()
			}
		}
	}()
}

// Close shuts down the heartbeat batcher (flushing pending writes) and the
// backing store, if any. Safe to call even without a store.
func (r *Registry) Close() error {
	if r.hbBatcher != nil {
		r.hbBatcher.Stop()
	}
	if r.store != nil {
		return r.store.Close()
	}
	return nil
}

// sweepExpired marks all online nodes whose heartbeat has expired as offline.
func (r *Registry) sweepExpired() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.nodes {
		if e.Status == StatusOnline && now.Sub(e.LastHeartbeat) > r.heartbeatTimeout {
			e.Status = StatusOffline
			e.stream = nil
		}
	}
}
