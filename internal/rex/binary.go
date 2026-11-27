package rex

import (
	"errors"
	"os"
	"path/filepath"
)

// MacAppBinary is where Rex.app keeps its CLI on macOS.
const MacAppBinary = "/Applications/Rex.app/Contents/Helpers/Rex Server.rex/Contents/MacOS/rex"

const (
	// macServerBinary is where the macOS app stages the running server's copy
	// under HOME. Checking it before the app bundle matches the live server
	// and survives app bundle renames (such as Rex Beta.app to Rex.app).
	macServerBinary  = "Library/Application Support/com.superlogical.rex/RexServer/Versions/Current/Rex Server.app/Contents/MacOS/rex"
	macBetaAppBinary = "/Applications/Rex Beta.app/Contents/Helpers/Rex Server.rex/Contents/MacOS/rex"
)

// ErrNoBinary means no rex binary was found anywhere FindBinary looks.
var ErrNoBinary = errors.New("rex: no rex binary in REX_BIN_DIR, PATH or " + MacAppBinary)

// FindBinary returns the rex binary to run. REX_BIN_DIR comes first because
// inside a pane it holds the rex that matches the running server, which can
// be older than the app's. Processes spawned by the server itself (such as
// init.lua actions) do not get REX_BIN_DIR or rex on PATH, so the staged
// server copy under HOME comes next, before the macOS app bundles.
func FindBinary(getenv func(string) string) (string, error) {
	return findBinary(getenv, MacAppBinary, macBetaAppBinary)
}

func findBinary(getenv func(string) string, appBinaries ...string) (string, error) {
	var candidates []string
	if dir := getenv("REX_BIN_DIR"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "rex"))
	}
	for _, dir := range filepath.SplitList(getenv("PATH")) {
		if dir != "" {
			candidates = append(candidates, filepath.Join(dir, "rex"))
		}
	}
	if home := getenv("HOME"); home != "" {
		candidates = append(candidates, filepath.Join(home, macServerBinary))
	}
	candidates = append(candidates, appBinaries...)
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
