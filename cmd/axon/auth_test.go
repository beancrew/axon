package main

import (
	"strings"
	"testing"
)

func TestAuthCmdHelp(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"auth", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth help failed: %v", err)
	}
}

func TestAuthLoginHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"auth", "login", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth login help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "username") {
		t.Errorf("help output missing 'username': %s", output)
	}
	if !strings.Contains(output, "--server") {
		t.Errorf("help output missing '--server': %s", output)
	}
}

func TestAuthTokenHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"auth", "token", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth token help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "token") {
		t.Errorf("help output missing 'token': %s", output)
	}
}

func TestRootHelpIncludesNodeAndAuth(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"node", "auth", "config", "version"} {
		if !strings.Contains(output, want) {
			t.Errorf("help output missing %q: %s", want, output)
		}
	}
}
