//go:build darwin

package agent

import (
	"os/exec"
	"strings"

	controlpb "github.com/beancrew/axon/gen/proto/control"
)

func collectOSInfo() *controlpb.OSInfo {
	info := &controlpb.OSInfo{
		Os:       "darwin",
		Platform: "macOS",
	}

	// Kernel version from uname.
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		info.OsVersion = strings.TrimSpace(string(out))
	}

	// macOS version from sw_vers.
	if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
		info.PlatformVersion = strings.TrimSpace(string(out))
	}

	// Build a pretty name.
	info.PrettyName = "macOS " + info.PlatformVersion

	return info
}
