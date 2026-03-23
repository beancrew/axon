//go:build windows

package agent

import (
	controlpb "github.com/garysng/axon/gen/proto/control"
)

func collectOSInfo() *controlpb.OSInfo {
	return &controlpb.OSInfo{
		Os:       "windows",
		Platform: "Windows",
	}
}
