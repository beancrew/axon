package main

import (
	"strings"
	"testing"
)

func TestExecCmdHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"exec", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("exec help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "command") {
		t.Errorf("exec help missing 'command': %s", output)
	}
	if !strings.Contains(output, "--timeout") {
		t.Errorf("exec help missing '--timeout': %s", output)
	}
	if !strings.Contains(output, "--env") {
		t.Errorf("exec help missing '--env': %s", output)
	}
	if !strings.Contains(output, "--workdir") {
		t.Errorf("exec help missing '--workdir': %s", output)
	}
}

func TestExecCmdRequiresArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"exec"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided to exec")
	}
}

func TestExecCmdRequiresCommand(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"exec", "node1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when only node provided to exec")
	}
}

func TestReadCmdHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"read", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("read help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Read") {
		t.Errorf("read help missing 'Read': %s", output)
	}
	if !strings.Contains(output, "--meta") {
		t.Errorf("read help missing '--meta': %s", output)
	}
}

func TestReadCmdRequiresArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"read"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided to read")
	}
}

func TestWriteCmdHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"write", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("write help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Write") {
		t.Errorf("write help missing 'Write': %s", output)
	}
	if !strings.Contains(output, "--mode") {
		t.Errorf("write help missing '--mode': %s", output)
	}
}

func TestWriteCmdRequiresArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"write"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided to write")
	}
}

func TestForwardCmdHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"forward", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("forward help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "forward") {
		t.Errorf("forward help missing 'forward': %s", output)
	}
	if !strings.Contains(output, "--bind") {
		t.Errorf("forward help missing '--bind': %s", output)
	}
}

func TestForwardCmdRequiresArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"forward"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided to forward")
	}
}

func TestParsePorts(t *testing.T) {
	tests := []struct {
		spec       string
		wantLocal  int32
		wantRemote int32
		wantErr    bool
	}{
		{"5432:5432", 5432, 5432, false},
		{"8080:80", 8080, 80, false},
		{"1:65535", 1, 65535, false},
		{"bad", 0, 0, true},
		{"0:80", 0, 0, true},
		{"80:0", 0, 0, true},
		{"70000:80", 0, 0, true},
		{"80:70000", 0, 0, true},
		{"abc:80", 0, 0, true},
		{"80:abc", 0, 0, true},
	}

	for _, tt := range tests {
		local, remote, err := parsePorts(tt.spec)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parsePorts(%q) expected error", tt.spec)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePorts(%q) unexpected error: %v", tt.spec, err)
			continue
		}
		if local != tt.wantLocal || remote != tt.wantRemote {
			t.Errorf("parsePorts(%q) = (%d, %d), want (%d, %d)", tt.spec, local, remote, tt.wantLocal, tt.wantRemote)
		}
	}
}

func TestRootHelpIncludesOpsCommands(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"exec", "read", "write", "forward"} {
		if !strings.Contains(output, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}
