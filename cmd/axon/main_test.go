package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beancrew/axon/pkg/config"
)

func TestVersionCmd(t *testing.T) {
	cmd := rootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	// Version output goes to os.Stdout via fmt.Printf, not cmd.Out.
	// Just verify the command runs without error.
}

func TestConfigSetGet(t *testing.T) {
	// Use a temp config file.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Write initial config.
	cfg := &config.CLIConfig{
		ServerAddr:   "old-server:50051",
		OutputFormat: "table",
	}
	if err := config.SaveCLIConfig(cfgPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	// Load and verify.
	loaded, err := config.LoadCLIConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.ServerAddr != "old-server:50051" {
		t.Errorf("ServerAddr = %q, want %q", loaded.ServerAddr, "old-server:50051")
	}

	// Update and verify.
	loaded.ServerAddr = "new-server:443"
	if err := config.SaveCLIConfig(cfgPath, loaded); err != nil {
		t.Fatalf("save updated config: %v", err)
	}

	reloaded, err := config.LoadCLIConfig(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.ServerAddr != "new-server:443" {
		t.Errorf("ServerAddr = %q, want %q", reloaded.ServerAddr, "new-server:443")
	}

	// Verify file permissions.
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("config file mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestConfigGetUnknownKey(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"config", "get", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown config key")
	}
	if !strings.Contains(err.Error(), "unknown config key") {
		t.Errorf("error = %q, expected to contain 'unknown config key'", err.Error())
	}
}

func TestConfigSetUnknownKey(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"config", "set", "nonexistent", "value"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown config key")
	}
}

func TestRootHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "axon") {
		t.Errorf("help output missing 'axon': %s", output)
	}
	if !strings.Contains(output, "config") {
		t.Errorf("help output missing 'config': %s", output)
	}
	if !strings.Contains(output, "version") {
		t.Errorf("help output missing 'version': %s", output)
	}
}
