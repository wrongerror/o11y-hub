package vizier

import (
	"strings"
)

// cleanPath cleans and normalizes URL paths
func cleanPath(rawPath string) string {
	if rawPath == "" {
		return "/"
	}

	// Remove query parameters
	if idx := strings.Index(rawPath, "?"); idx != -1 {
		rawPath = rawPath[:idx]
	}

	// Remove fragment
	if idx := strings.Index(rawPath, "#"); idx != -1 {
		rawPath = rawPath[:idx]
	}

	// Ensure it starts with /
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}

	// Remove trailing slash except for root
	if len(rawPath) > 1 && strings.HasSuffix(rawPath, "/") {
		rawPath = strings.TrimSuffix(rawPath, "/")
	}

	return rawPath
}
