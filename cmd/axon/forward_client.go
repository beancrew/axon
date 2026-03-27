package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// daemonRequest is a JSON-encoded request sent over the unix socket.
type daemonRequest struct {
	Action     string `json:"action"`
	Node       string `json:"node,omitempty"`
	LocalPort  int    `json:"local_port,omitempty"`
	RemotePort int    `json:"remote_port,omitempty"`
	BindAddr   string `json:"bind_addr,omitempty"`
	ID         string `json:"id,omitempty"`
}

// daemonResponse is a JSON-encoded response received from the daemon.
type daemonResponse struct {
	OK       bool          `json:"ok"`
	ID       string        `json:"id,omitempty"`
	Message  string        `json:"message,omitempty"`
	Forwards []forwardInfo `json:"forwards,omitempty"`
}

// forwardInfo describes an active forward.
type forwardInfo struct {
	ID         string    `json:"id"`
	Node       string    `json:"node"`
	LocalPort  int       `json:"local_port"`
	RemotePort int       `json:"remote_port"`
	BindAddr   string    `json:"bind_addr"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// axonDataDir returns ~/.axon, creating it if necessary.
func axonDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".axon"), nil
}

func daemonSockPath() (string, error) {
	dir, err := axonDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "forward.sock"), nil
}

// sendDaemonRequest connects to the daemon, sends req, and returns the response.
func sendDaemonRequest(req daemonRequest) (*daemonResponse, error) {
	sockPath, err := daemonSockPath()
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp daemonResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &resp, nil
}

// ensureDaemon checks whether the daemon is running and auto-starts it if not.
func ensureDaemon() error {
	sockPath, err := daemonSockPath()
	if err != nil {
		return err
	}

	// Check if already running.
	conn, err := net.DialTimeout("unix", sockPath, time.Second)
	if err == nil {
		conn.Close()
		return nil
	}

	// Prepare data directory.
	dir, err := axonDataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Open log file for daemon stdout/stderr.
	logPath := filepath.Join(dir, "forward.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	// Re-exec self as daemon, detached from current session.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	cmd := exec.Command(exe, "forward", "daemon", "--foreground")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Write PID file as a convenience (daemon also writes it).
	pidPath := filepath.Join(dir, "forward.pid")
	_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0600)

	// Wait up to 5 seconds for the daemon to be ready.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		c, err := net.DialTimeout("unix", sockPath, time.Second)
		if err == nil {
			c.Close()
			return nil
		}
	}
	return fmt.Errorf("daemon did not start within 5 seconds")
}

// daemonCreate auto-starts the daemon if needed, then creates a forward.
func daemonCreate(node string, localPort, remotePort int, bindAddr string) (string, error) {
	if err := ensureDaemon(); err != nil {
		return "", fmt.Errorf("start daemon: %w", err)
	}

	resp, err := sendDaemonRequest(daemonRequest{
		Action:     "create",
		Node:       node,
		LocalPort:  localPort,
		RemotePort: remotePort,
		BindAddr:   bindAddr,
	})
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", fmt.Errorf("%s", resp.Message)
	}
	return resp.ID, nil
}

// daemonList returns active forwards. Returns nil if the daemon is not running.
func daemonList() ([]forwardInfo, error) {
	resp, err := sendDaemonRequest(daemonRequest{Action: "list"})
	if err != nil {
		// Daemon not running — treat as empty list.
		return nil, nil
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Message)
	}
	return resp.Forwards, nil
}

// daemonDelete removes the forward with the given ID.
func daemonDelete(id string) error {
	resp, err := sendDaemonRequest(daemonRequest{Action: "delete", ID: id})
	if err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Message)
	}
	return nil
}
