package recipe

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// AgySessions finds an agy conversation in the log agy writes for every run,
// ~/.gemini/antigravity-cli/log/cli-YYYYMMDD_HHMMSS.log, named for when the
// run began in local time.
type AgySessions struct{}

const agyLogLayout = "cli-20060102_150405.log"

// FindSessionID returns the first conversation created by the newest run
// that began at or after q.Began with q.Dir as its workspace.
//
// agy's conversation_summaries.db can't serve: sqlite3 -readonly fails to
// open it, and it leaves workspace_uris empty for a short conversation.
func (AgySessions) FindSessionID(_ context.Context, q SessionQuery) (string, bool, error) {
	dir := filepath.Join(q.Home, ".gemini", "antigravity-cli", "log")
	paths, err := filepath.Glob(filepath.Join(dir, "cli-*.log"))
	if err != nil {
		return "", false, fmt.Errorf("listing agy's logs: %w", err)
	}
	// Names have whole seconds; a run in q.Began's own second is named earlier.
	since := q.Began.Truncate(time.Second)
	var runs []agyRun
	for _, path := range paths {
		began, err := time.ParseInLocation(agyLogLayout, filepath.Base(path), time.Local)
		if err == nil && !began.Before(since) {
			runs = append(runs, agyRun{path: path, began: began})
		}
	}
	slices.SortFunc(runs, func(a, b agyRun) int { return b.began.Compare(a.began) })
	for _, run := range runs {
		workspace, conversation, err := readAgyLog(run.path)
		if err != nil {
			return "", false, err
		}
		if conversation != "" && sameDir(workspace, q.Dir) {
			return conversation, true, nil
		}
	}
	return "", false, nil
}

type agyRun struct {
	path  string
	began time.Time
}

// readAgyLog reads a run's workspace and first conversation from lines like
//
//	... Creating CLI server backend: product=antigravity workspaceDirs=[/r/app] appDataDir=...
//	... Created conversation 56fc796c-36fc-4ce8-a6ea-d2b478877c19 (backend=...)
func readAgyLog(path string) (workspace, conversation string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("reading agy's log: %w", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() && (workspace == "" || conversation == "") {
		line := scanner.Text()
		if _, rest, ok := strings.Cut(line, " workspaceDirs=["); ok && workspace == "" {
			workspace, _, _ = strings.Cut(rest, "] ")
		}
		if _, rest, ok := strings.Cut(line, "] Created conversation "); ok && conversation == "" {
			conversation, _, _ = strings.Cut(rest, " ")
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("reading agy's log %s: %w", path, err)
	}
	return workspace, conversation, nil
}
