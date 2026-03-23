// Package agent implements the Axon agent control plane.
package agent

import (
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	controlpb "github.com/garysng/axon/gen/proto/control"
)

// version is set at build time via -ldflags.
var version = "dev"

// collectNodeInfo gathers hardware and OS information for the current host.
func collectNodeInfo() *controlpb.NodeInfo {
	hostname, _ := os.Hostname()
	uptime := uptimeSeconds()

	return &controlpb.NodeInfo{
		Hostname:      hostname,
		Arch:          runtime.GOARCH,
		Ip:            primaryIP(),
		UptimeSeconds: uptime,
		AgentVersion:  version,
		OsInfo:        collectOSInfo(),
	}
}

// primaryIP returns the first non-loopback IPv4 address found on the host.
func primaryIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		return ip.String()
	}
	return ""
}

// uptimeSeconds returns the host uptime in seconds. Platform-specific
// implementations are in sysinfo_*.go files.
// This is a fallback that returns process uptime.
var processStart = time.Now()

func uptimeSeconds() int64 {
	return int64(time.Since(processStart).Seconds())
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
