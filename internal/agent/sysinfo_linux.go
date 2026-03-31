//go:build linux

package agent

import (
	"os"
	"os/exec"
	"strings"

	controlpb "github.com/beancrew/axon/gen/proto/control"
)

func collectOSInfo() *controlpb.OSInfo {
	info := &controlpb.OSInfo{
		Os: "linux",
	}

	// Kernel version from uname.
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		info.OsVersion = strings.TrimSpace(string(out))
	}

	// Distribution info from /etc/os-release.
	kv := parseKeyValueFile("/etc/os-release")
	if kv != nil {
		info.Platform = strings.ToLower(kv["ID"])
		info.PlatformVersion = kv["VERSION_ID"]
		info.PrettyName = kv["PRETTY_NAME"]
	}

	return info
}

// parseKeyValueFile reads a file of KEY=VALUE lines (like /etc/os-release)
// and returns a map of the values with optional quotes stripped.
func parseKeyValueFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	result := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := strings.Trim(parts[1], `"'`)
		result[key] = val
	}
	return result
}
