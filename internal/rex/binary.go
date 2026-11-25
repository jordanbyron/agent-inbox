package rex

import (
	"errors"
	"os"
	"path/filepath"
)

// MacAppBinary is where the Rex app keeps its CLI on macOS.
const MacAppBinary = "/Applications/Rex Beta.app/Contents/Helpers/Rex Server.rex/Contents/MacOS/rex"

// ErrNoBinary means no rex binary was found anywhere FindBinary looks.
var ErrNoBinary = errors.New("rex: no rex binary in REX_BIN_DIR, PATH or " + MacAppBinary)

// FindBinary returns the rex binary to run. REX_BIN_DIR comes first because
// inside a pane it holds the rex that matches the running server, which can
// be older than the app's.
func FindBinary(getenv func(string) string) (string, error) {
	return findBinary(getenv, MacAppBinary)
}

func findBinary(getenv func(string) string, appBinary string) (string, error) {
	var candidates []string
	if dir := getenv("REX_BIN_DIR"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "rex"))
	}
	for _, dir := range filepath.SplitList(getenv("PATH")) {
		if dir != "" {
			candidates = append(candidates, filepath.Join(dir, "rex"))
		}
	}
	candidates = append(candidates, appBinary)
	for _, path := range candidates {
		if isExecutable(path) {
			return path, nil
		}
	}
	return "", ErrNoBinary
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
