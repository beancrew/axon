//go:build linux

package agent

import (
	"os/exec"
	"strings"

	controlpb "github.com/garysng/axon/gen/proto/control"
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
