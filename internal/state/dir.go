package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Dir returns the state directory for the Rex server with the given id,
// creating it with mode 0700 if it does not exist.
func Dir(server string) (string, error) {
	if server == "" || server == "." || server == ".." || strings.Contains(server, "/") {
		return "", fmt.Errorf("finding the state directory: %q is not a server id", server)
	}
	base := os.Getenv("XDG_STATE_HOME")
	// The XDG spec says to ignore a relative path here.
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding the state directory: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "agent-inbox", server)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating the state directory: %w", err)
	}
	return dir, nil
}
