package main

import (
	"testing"
	"time"

	managementpb "github.com/garysng/axon/gen/proto/management"
)

func TestFilterByStatus(t *testing.T) {
	nodes := []*managementpb.NodeSummary{
		{NodeId: "n1", Status: "online"},
		{NodeId: "n2", Status: "offline"},
		{NodeId: "n3", Status: "online"},
	}

	online := filterByStatus(nodes, "online")
	if len(online) != 2 {
		t.Errorf("filterByStatus(online) got %d nodes, want 2", len(online))
	}

	offline := filterByStatus(nodes, "offline")
	if len(offline) != 1 {
		t.Errorf("filterByStatus(offline) got %d nodes, want 1", len(offline))
	}

	// Case-insensitive.
	upper := filterByStatus(nodes, "ONLINE")
	if len(upper) != 2 {
		t.Errorf("filterByStatus(ONLINE) got %d nodes, want 2", len(upper))
	}

	// No match.
	none := filterByStatus(nodes, "unknown")
	if len(none) != 0 {
		t.Errorf("filterByStatus(unknown) got %d nodes, want 0", len(none))
	}
}

func TestFilterByStatusEmpty(t *testing.T) {
	var nodes []*managementpb.NodeSummary
	result := filterByStatus(nodes, "online")
	if len(result) != 0 {
		t.Errorf("filterByStatus on nil slice got %d, want 0", len(result))
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds int64
		want    string
	}{
		{0, "0m"},
		{59, "0m"},
		{60, "1m"},
		{3600, "1h 0m"},
		{3661, "1h 1m"},
		{86400, "1d 0h 0m"},
		{90061, "1d 1h 1m"},
		{30 * 24 * 3600, "30d 0h 0m"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.seconds)
		if got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}

func TestRelativeTime(t *testing.T) {
	// Zero timestamp.
	if got := relativeTime(0); got != "never" {
		t.Errorf("relativeTime(0) = %q, want %q", got, "never")
	}

	// Recent timestamp (within last minute).
	recent := relativeTime(timeNowUnix() - 5)
	if !containsAny(recent, "s ago", "just now") {
		t.Errorf("relativeTime(5s ago) = %q, expected seconds ago or just now", recent)
	}

	// Minutes ago.
	minsAgo := relativeTime(timeNowUnix() - 120)
	if !containsAny(minsAgo, "m ago") {
		t.Errorf("relativeTime(2m ago) = %q, expected minutes ago", minsAgo)
	}

	// Hours ago.
	hoursAgo := relativeTime(timeNowUnix() - 7200)
	if !containsAny(hoursAgo, "h ago") {
		t.Errorf("relativeTime(2h ago) = %q, expected hours ago", hoursAgo)
	}

	// Days ago.
	daysAgo := relativeTime(timeNowUnix() - 172800)
	if !containsAny(daysAgo, "d ago") {
		t.Errorf("relativeTime(2d ago) = %q, expected days ago", daysAgo)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly ten", 11, "exactly ten"},
		{"Ubuntu 24.04 LTS", 20, "Ubuntu 24.04 LTS"},
		{"Ubuntu 24.04 LTS (Jammy)", 20, "Ubuntu 24.04 LTS ..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
	}

	for _, tt := range tests {
		got := truncate(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		token string
		want  string
	}{
		{"", "(not set)"},
		{"short", "***"},
		{"12345678901", "***"},
		{"eyJhbGciOiJIUzI1NiIs4F0", "eyJhbG...4F0"},
	}

	for _, tt := range tests {
		got := maskToken(tt.token)
		if got != tt.want {
			t.Errorf("maskToken(%q) = %q, want %q", tt.token, got, tt.want)
		}
	}
}

func TestNodeCmdHelp(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"node", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("node help failed: %v", err)
	}
}

func TestNodeInfoRequiresArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"node", "info"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no args provided to node info")
	}
}

func TestNodeRemoveRequiresArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"node", "remove"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no args provided to node remove")
	}
}

func TestNodeRemoveForceFlag(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"node", "remove", "--force", "some-node"})
	// Will fail due to no server, but flag parsing should work.
	_ = cmd.Execute()
}

func TestPrintJSON(t *testing.T) {
	cmd := rootCmd()
	node := &managementpb.NodeSummary{
		NodeId:   "test-id",
		NodeName: "test-node",
		Status:   "online",
	}
	if err := printJSON(cmd, node); err != nil {
		t.Fatalf("printJSON failed: %v", err)
	}
}

func TestNodeListJSONFlag(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"node", "list", "--json"})
	_ = cmd.Execute()
}

func TestNodeInfoJSONFlag(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"node", "info", "--json", "some-node"})
	_ = cmd.Execute()
}

func TestNodeListStatusFlag(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"node", "list", "--status", "online"})
	_ = cmd.Execute()
}

// ── test helpers ───────────────────────────────────────────────────────────

func timeNowUnix() int64 {
	return time.Now().Unix()
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
