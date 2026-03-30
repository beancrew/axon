package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	operationspb "github.com/garysng/axon/gen/proto/operations"
)

const daemonIdleTimeout = 60 * time.Second

// forwardEntry tracks a single active port forward managed by the daemon.
type forwardEntry struct {
	ID         string
	Node       string
	LocalPort  int
	RemotePort int
	BindAddr   string
	Status     string
	CreatedAt  time.Time
	listener   net.Listener
	cancel     context.CancelFunc
}

// forwardManager owns all active forwards and the shared gRPC client.
type forwardManager struct {
	mu          sync.Mutex
	forwards    map[string]*forwardEntry
	client      operationspb.OperationsServiceClient
	clientClose func()
	idleTimer   *time.Timer
	exitCh      chan struct{}
	exitOnce    sync.Once
}

func newForwardManager() *forwardManager {
	fm := &forwardManager{
		forwards: make(map[string]*forwardEntry),
		exitCh:   make(chan struct{}),
	}
	// Start idle timer — fires if no forwards are created in the first 60s.
	fm.idleTimer = time.AfterFunc(daemonIdleTimeout, func() {
		fm.exitOnce.Do(func() { close(fm.exitCh) })
	})
	return fm
}

// resetIdleTimer restarts (empty forwards) or stops (busy) the idle timer.
// Must be called with mu held.
func (fm *forwardManager) resetIdleTimer() {
	if len(fm.forwards) == 0 {
		fm.idleTimer.Reset(daemonIdleTimeout)
	} else {
		fm.idleTimer.Stop()
	}
}

// getOrCreateClient returns the shared gRPC client, creating it if needed.
// Must be called with mu held.
func (fm *forwardManager) getOrCreateClient() (operationspb.OperationsServiceClient, error) {
	if fm.client != nil {
		return fm.client, nil
	}
	client, closer, err := dialOperations()
	if err != nil {
		return nil, err
	}
	fm.client = client
	fm.clientClose = closer
	return client, nil
}

func (fm *forwardManager) handleCreate(req daemonRequest) *daemonResponse {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	client, err := fm.getOrCreateClient()
	if err != nil {
		return &daemonResponse{OK: false, Message: err.Error()}
	}

	bindAddr := req.BindAddr
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}

	listenAddr := fmt.Sprintf("%s:%d", bindAddr, req.LocalPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return &daemonResponse{OK: false, Message: fmt.Sprintf("listen on %s: %v", listenAddr, err)}
	}

	id := randomHexID()
	ctx, cancel := context.WithCancel(context.Background())

	entry := &forwardEntry{
		ID:         id,
		Node:       req.Node,
		LocalPort:  req.LocalPort,
		RemotePort: req.RemotePort,
		BindAddr:   bindAddr,
		Status:     "active",
		CreatedAt:  time.Now(),
		listener:   listener,
		cancel:     cancel,
	}
	fm.forwards[id] = entry
	fm.resetIdleTimer()

	go fm.acceptLoop(ctx, entry, client)

	return &daemonResponse{OK: true, ID: id}
}

func (fm *forwardManager) handleList() *daemonResponse {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	infos := make([]forwardInfo, 0, len(fm.forwards))
	for _, e := range fm.forwards {
		infos = append(infos, forwardInfo{
			ID:         e.ID,
			Node:       e.Node,
			LocalPort:  e.LocalPort,
			RemotePort: e.RemotePort,
			BindAddr:   e.BindAddr,
			Status:     e.Status,
			CreatedAt:  e.CreatedAt,
		})
	}
	return &daemonResponse{OK: true, Forwards: infos}
}

func (fm *forwardManager) handleDelete(id string) *daemonResponse {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	entry, ok := fm.forwards[id]
	if !ok {
		return &daemonResponse{OK: false, Message: fmt.Sprintf("forward %s not found", id)}
	}

	entry.cancel()
	_ = entry.listener.Close()
	delete(fm.forwards, id)
	fm.resetIdleTimer()

	return &daemonResponse{OK: true, Message: "Forward deleted"}
}

// acceptLoop runs for one forwardEntry, accepting TCP connections and
// handing each to handleForwardConn in its own goroutine.
func (fm *forwardManager) acceptLoop(ctx context.Context, entry *forwardEntry, client operationspb.OperationsServiceClient) {
	go func() {
		<-ctx.Done()
		_ = entry.listener.Close()
	}()

	for {
		conn, err := entry.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return // cancelled — expected
			}
			fm.mu.Lock()
			if e, ok := fm.forwards[entry.ID]; ok {
				e.Status = "failed"
			}
			fm.mu.Unlock()
			return
		}
		go handleForwardConn(ctx, client, conn, entry.Node, int32(entry.RemotePort))
	}
}

func (fm *forwardManager) close() {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.idleTimer.Stop()
	for _, entry := range fm.forwards {
		entry.cancel()
		_ = entry.listener.Close()
	}
	if fm.clientClose != nil {
		fm.clientClose()
	}
}

// randomHexID returns a random 8-character hex string.
func randomHexID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return fmt.Sprintf("%x", b)
}

// runDaemon is the entry point for the forward daemon subprocess.
func runDaemon() error {
	dir, err := axonDataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	sockPath := filepath.Join(dir, "forward.sock")
	pidPath := filepath.Join(dir, "forward.pid")

	// Remove stale socket from a previous run.
	_ = os.Remove(sockPath)

	// Record our PID.
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0600); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", sockPath, err)
	}

	defer func() {
		_ = ln.Close()
		_ = os.Remove(sockPath)
		_ = os.Remove(pidPath)
	}()

	fm := newForwardManager()
	defer fm.close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Accept unix-socket connections in background.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleDaemonConn(conn, fm)
		}
	}()

	// Block until signalled or idle timeout.
	select {
	case sig := <-sigCh:
		_, _ = fmt.Fprintf(os.Stderr, "[forward daemon] received signal %s, shutting down\n", sig)
	case <-fm.exitCh:
		_, _ = fmt.Fprintln(os.Stderr, "[forward daemon] idle timeout, shutting down")
	}

	return nil
}

// handleDaemonConn processes a single request-response cycle on the unix socket.
func handleDaemonConn(conn net.Conn, fm *forwardManager) {
	defer func() { _ = conn.Close() }()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req daemonRequest
	if err := dec.Decode(&req); err != nil {
		return
	}

	var resp *daemonResponse
	switch req.Action {
	case "create":
		resp = fm.handleCreate(req)
	case "list":
		resp = fm.handleList()
	case "delete":
		resp = fm.handleDelete(req.ID)
	default:
		resp = &daemonResponse{OK: false, Message: fmt.Sprintf("unknown action: %s", req.Action)}
	}

	if err := enc.Encode(resp); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "[forward daemon] failed to write response: %v\n", err)
	}
}
