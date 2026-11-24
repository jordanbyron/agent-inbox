package recipe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// AgySessions finds an agy conversation in its conversation_summaries.db,
// read with the sqlite3 program so the build needs no SQLite library.
type AgySessions struct {
	SQLite string
}

// The query takes no input, so nothing can be injected into it. A
// sub-agent's conversation has a parent and is never the one to resume.
const agyQuery = `SELECT conversation_id, workspace_uris, last_modified_time
FROM conversation_summaries WHERE parent_conversation_id = ''`

// agy writes times as "2026-11-24 01:06:39.488679+00:00".
const agyTimeLayout = "2006-01-02 15:04:05.999999999Z07:00"

type agyRow struct {
	ID            string `json:"conversation_id"`
	WorkspaceURIs string `json:"workspace_uris"`
	Modified      string `json:"last_modified_time"`
}

// FindSessionID returns the newest conversation whose workspace is q.Dir
// modified at or after q.Began. With no sqlite3 or no database it reports
// false rather than an error.
func (a AgySessions) FindSessionID(ctx context.Context, q SessionQuery) (string, bool, error) {
	db := filepath.Join(q.Home, ".gemini", "antigravity-cli", "conversation_summaries.db")
	if _, err := os.Stat(db); errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	sqlite, err := exec.LookPath(a.SQLite)
	if err != nil {
		return "", false, nil
	}
	rows, err := queryAgy(ctx, sqlite, db)
	if err != nil {
		return "", false, err
	}
	var newestID string
	var newest time.Time
	for _, row := range rows {
		modified, err := time.Parse(agyTimeLayout, row.Modified)
		if err != nil {
			return "", false, fmt.Errorf("reading agy conversation %s: %w", row.ID, err)
		}
		if modified.Before(q.Began) || !modified.After(newest) || !inWorkspace(row.WorkspaceURIs, q.Dir) {
			continue
		}
		newestID, newest = row.ID, modified
	}
	return newestID, newestID != "", nil
}

func queryAgy(ctx context.Context, sqlite, db string) ([]agyRow, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, sqlite, "-readonly", "-json", db, agyQuery)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("reading agy conversations: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	// sqlite3 -json prints nothing at all for no rows.
	if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
		return nil, nil
	}
	var rows []agyRow
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		return nil, fmt.Errorf("reading agy conversations: %w", err)
	}
	return rows, nil
}

// inWorkspace reports whether dir is one of a conversation's workspaces,
// which agy keeps as a JSON array of file:// URIs, or an empty string.
func inWorkspace(uris, dir string) bool {
	var list []string
	if json.Unmarshal([]byte(uris), &list) != nil {
		return false
	}
	for _, raw := range list {
		u, err := url.Parse(raw)
		if err == nil && u.Scheme == "file" && sameDir(u.Path, dir) {
			return true
		}
	}
	return false
}
