package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/garysng/axon/pkg/display"
)

// ── writePID / readPID ──────────────────────────────────────────────────────

func TestWriteReadPIDRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.pid")
	if err := writePID(path, 42); err != nil {
		t.Fatalf("writePID: %v", err)
	}
	pid, err := readPID(path)
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != 42 {
		t.Errorf("got pid %d, want 42", pid)
	}
}

func TestReadPIDInvalidContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.pid")
	if err := os.WriteFile(path, []byte("not-a-number\n"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := readPID(path)
	if err == nil {
		t.Fatal("expected error for invalid content, got nil")
	}
}

func TestReadPIDMissingFile(t *testing.T) {
	_, err := readPID(filepath.Join(t.TempDir(), "nonexistent.pid"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

// ── isProcessRunning ────────────────────────────────────────────────────────

func TestIsProcessRunningCurrentPID(t *testing.T) {
	if !isProcessRunning(os.Getpid()) {
		t.Error("current process should be detected as running")
	}
}

func TestIsProcessRunningNonExistentPID(t *testing.T) {
	if isProcessRunning(999_999_999) {
		t.Error("PID 999999999 should not be detected as running")
	}
}

// ── maskToken ───────────────────────────────────────────────────────────────

func TestMaskToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "(not set)"},
		{"short", "***"},           // len < 12
		{"11chars_str", "***"},     // len == 11 < 12
		{"123456789012", "123456...012"}, // len == 12
		{"abcdefghijklmnopqrs", "abcdef...qrs"}, // normal length
	}
	for _, tc := range tests {
		got := display.MaskToken(tc.input)
		if got != tc.want {
			t.Errorf("display.MaskToken(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── formatUptime ────────────────────────────────────────────────────────────

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "0m"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{25*time.Hour + 5*time.Minute, "1d 1h 5m"},
		{3*24*time.Hour + 12*time.Hour + 5*time.Minute, "3d 12h 5m"},
	}
	for _, tc := range tests {
		got := formatUptime(tc.d)
		if got != tc.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// ── command help ────────────────────────────────────────────────────────────

func TestStartCmdHelp(t *testing.T) {
	var buf bytes.Buffer
	cmd := startCmd()
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("Help(): %v", err)
	}
	out := buf.String()
	for _, want := range []string{"start", "--server", "--foreground"} {
		if !strings.Contains(out, want) {
			t.Errorf("start help missing %q:\n%s", want, out)
		}
	}
}

func TestStopCmdHelp(t *testing.T) {
	var buf bytes.Buffer
	cmd := stopCmd()
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("Help(): %v", err)
	}
	if !strings.Contains(buf.String(), "stop") {
		t.Errorf("stop help missing 'stop':\n%s", buf.String())
	}
}

func TestStatusCmdHelp(t *testing.T) {
	var buf bytes.Buffer
	cmd := statusCmd()
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("Help(): %v", err)
	}
	out := buf.String()
	for _, want := range []string{"status", "--json"} {
		if !strings.Contains(out, want) {
			t.Errorf("status help missing %q:\n%s", want, out)
		}
	}
}

func TestConfigCmdHelp(t *testing.T) {
	var buf bytes.Buffer
	cmd := agentConfigCmd()
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("Help(): %v", err)
	}
	if !strings.Contains(buf.String(), "config") {
		t.Errorf("config help missing 'config':\n%s", buf.String())
	}
}

func TestVersionCmdHelp(t *testing.T) {
	var buf bytes.Buffer
	cmd := versionCmd()
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("Help(): %v", err)
	}
	if !strings.Contains(buf.String(), "version") {
		t.Errorf("version help missing 'version':\n%s", buf.String())
	}
}

func TestRootCmdHelpListsSubcommands(t *testing.T) {
	var buf bytes.Buffer
	cmd := rootCmd()
	cmd.SetOut(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("Help(): %v", err)
	}
	out := buf.String()
	for _, sub := range []string{"start", "stop", "status", "config", "version"} {
		if !strings.Contains(out, sub) {
			t.Errorf("root help missing subcommand %q:\n%s", sub, out)
		}
	}
}
