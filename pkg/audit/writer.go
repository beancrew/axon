package audit

import (
	"log"
	"sync"
)

const defaultBatchSize = 64

// Writer is an asynchronous, buffered audit log writer backed by a Store.
// Log calls are non-blocking: entries are queued onto an internal channel and
// flushed to the store by a background goroutine in batches.
// When the buffer is full the oldest pending entry is dropped and a warning is
// logged so that callers are never blocked.
type Writer struct {
	store *Store
	ch    chan AuditEntry
	done  chan struct{}
	wg    sync.WaitGroup
}

// NewWriter creates a Writer with an internal channel of capacity bufferSize.
// It starts a background goroutine immediately.
func NewWriter(store *Store, bufferSize int) *Writer {
	if bufferSize <= 0 {
		bufferSize = defaultBatchSize
	}
	w := &Writer{
		store: store,
		ch:    make(chan AuditEntry, bufferSize),
		done:  make(chan struct{}),
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// Log enqueues entry for asynchronous persistence. It never blocks:
// if the internal buffer is full the entry is dropped and a warning is printed.
func (w *Writer) Log(entry AuditEntry) {
	select {
	case w.ch <- entry:
	default:
		log.Printf("audit: writer buffer full, dropping entry (op=%s user=%s node=%s)",
			entry.Operation, entry.UserID, entry.NodeID)
	}
}

// Close flushes all buffered entries to the store and waits for the background
// goroutine to exit before returning.
func (w *Writer) Close() error {
	close(w.ch)
	w.wg.Wait()
	return nil
}

// run is the background goroutine.  It drains the channel in batches and
// writes each batch to the store.
func (w *Writer) run() {
	defer w.wg.Done()

	batch := make([]AuditEntry, 0, defaultBatchSize)

	flush := func() {
		for _, e := range batch {
			if err := w.store.Insert(e); err != nil {
				log.Printf("audit: failed to persist entry: %v", err)
			}
		}
		batch = batch[:0]
	}

	for entry := range w.ch {
		batch = append(batch, entry)
		// Drain any immediately available entries into the same batch.
	drain:
		for {
			select {
			case e, ok := <-w.ch:
				if !ok {
					// Channel closed while draining — flush and exit.
					flush()
					return
				}
				batch = append(batch, e)
			default:
				break drain
			}
		}
		flush()
	}
	// Channel closed; flush any remaining entries.
	flush()
}
