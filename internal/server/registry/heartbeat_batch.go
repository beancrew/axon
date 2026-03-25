package registry

import (
	"log"
	"sync"
	"time"
)

const defaultFlushInterval = 30 * time.Second

// heartbeatBatcher aggregates heartbeat timestamps in memory and flushes
// them to the Store periodically. This avoids writing to SQLite on every
// heartbeat (which could be thousands per minute for many nodes).
type heartbeatBatcher struct {
	store    Store
	interval time.Duration

	mu      sync.Mutex
	pending map[string]time.Time // nodeID → latest heartbeat time

	done chan struct{}
	wg   sync.WaitGroup
}

func newHeartbeatBatcher(store Store, interval time.Duration) *heartbeatBatcher {
	if interval <= 0 {
		interval = defaultFlushInterval
	}
	return &heartbeatBatcher{
		store:    store,
		interval: interval,
		pending:  make(map[string]time.Time),
		done:     make(chan struct{}),
	}
}

// Record stores a heartbeat timestamp for later batch persistence.
func (b *heartbeatBatcher) Record(nodeID string, t time.Time) {
	b.mu.Lock()
	b.pending[nodeID] = t
	b.mu.Unlock()
}

// Start begins the background flush loop. Call Stop to shut down gracefully.
func (b *heartbeatBatcher) Start() {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(b.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				b.flush()
			case <-b.done:
				b.flush() // Final flush on shutdown.
				return
			}
		}
	}()
}

// Stop signals the batcher to flush remaining entries and exit.
func (b *heartbeatBatcher) Stop() {
	close(b.done)
	b.wg.Wait()
}

// flush writes all pending heartbeat timestamps to the store in one transaction.
func (b *heartbeatBatcher) flush() {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.pending
	b.pending = make(map[string]time.Time)
	b.mu.Unlock()

	if err := b.store.FlushHeartbeats(batch); err != nil {
		log.Printf("registry: flush heartbeats: %v", err)
	}
}
