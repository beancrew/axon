// Package display provides shared formatting utilities for CLI output.
package display

// MaskToken returns a masked version of a token for safe display.
// Short tokens are fully masked; longer tokens show first 6 and last 3 chars.
func MaskToken(token string) string {
	if token == "" {
		return "(not set)"
	}
	if len(token) < 12 {
		return "***"
	}
	return token[:6] + "..." + token[len(token)-3:]
}
