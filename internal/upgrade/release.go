package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
)

const latestReleaseURL = "https://api.github.com/repos/jordanbyron/agent-inbox/releases/latest"

// GitHub asks GitHub for agent-inbox's latest release.
type GitHub struct {
	// URL is the latest-release endpoint; empty is agent-inbox's own.
	URL    string
	Client *http.Client
}

// Latest is the tag of the newest release.
func (g GitHub) Latest(ctx context.Context) (string, error) {
	url, client := g.URL, g.Client
	if url == "" {
		url = latestReleaseURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("asking for the latest release: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "agent-inbox")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("asking for the latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asking for the latest release: %s", resp.Status)
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("reading the latest release: %w", err)
	}
	return release.Tag, nil
}

// Newer reports whether latest is a later release than running. Only plain
// vX.Y.Z tags compare, so a build from a checkout is never offered one.
func Newer(latest, running string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}
	r, ok := parse(running)
	if !ok {
		return false
	}
	for i := range l {
		if l[i] != r[i] {
			return l[i] > r[i]
		}
	}
	return false
}

func parse(tag string) ([3]int, bool) {
	var v [3]int
	if _, err := fmt.Sscanf(tag, "v%d.%d.%d", &v[0], &v[1], &v[2]); err != nil {
		return v, false
	}
	return v, fmt.Sprintf("v%d.%d.%d", v[0], v[1], v[2]) == tag
}

// Homebrew returns the brew that installed the binary at exe, if one did.
// It is found from the keg's own prefix, since a list Rex starts may not
// have brew on its PATH.
func Homebrew(exe string) (brew string, ok bool) {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", false
	}
	prefix, _, ok := strings.Cut(resolved, "/Cellar/agent-inbox/")
	if !ok {
		return "", false
	}
	return filepath.Join(prefix, "bin", "brew"), true
}

// BrewUpgrade upgrades agent-inbox with brew.
func BrewUpgrade(brew string) error {
	out, err := exec.Command(brew, "upgrade", "jordanbyron/tap/agent-inbox").CombinedOutput()
	if err != nil {
		return fmt.Errorf("brew upgrade: %s", lastLine(out, err))
	}
	return nil
}

func lastLine(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if last := lines[len(lines)-1]; last != "" {
		return last
	}
	return err.Error()
}
