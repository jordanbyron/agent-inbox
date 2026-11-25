package recipe

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// TitleFinder reads the title a harness gave a conversation.
type TitleFinder interface {
	// FindTitle returns the conversation's latest title, and false when it
	// has none yet.
	FindTitle(ctx context.Context, q TitleQuery) (string, bool, error)
}

// TitleQuery names the conversation whose title to read, and the home
// directory that holds the harness's files.
type TitleQuery struct {
	Home      string
	SessionID string
}

// ClaudeTitles reads the title from a Claude Code transcript, under
// ~/.claude/projects/<the directory, encoded>/<session id>.jsonl.
type ClaudeTitles struct{}

// Claude Code writes an ai-title, and rewrites it as the work moves on,
// only for a session started without --name; --name, and /rename, write a
// custom-title instead. Either is re-appended often, so the last one wins.
var titleLines = [][]byte{[]byte(`{"type":"ai-title"`), []byte(`{"type":"custom-title"`)}

type titleLine struct {
	AITitle     string `json:"aiTitle"`
	CustomTitle string `json:"customTitle"`
}

// FindTitle returns the last title in the session's transcript.
func (ClaudeTitles) FindTitle(_ context.Context, q TitleQuery) (string, bool, error) {
	if q.SessionID == "" {
		return "", false, nil
	}
	paths, err := filepath.Glob(filepath.Join(q.Home, ".claude", "projects", "*", q.SessionID+".jsonl"))
	if err != nil || len(paths) == 0 {
		return "", false, err
	}
	f, err := os.Open(paths[0])
	if err != nil {
		return "", false, fmt.Errorf("reading the Claude transcript: %w", err)
	}
	defer func() { _ = f.Close() }()
	title := ""
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if isTitleLine(line) {
			var t titleLine
			if json.Unmarshal(line, &t) == nil {
				if found := cmp.Or(t.CustomTitle, t.AITitle); found != "" {
					title = found
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return title, title != "", nil
		}
		if err != nil {
			return "", false, fmt.Errorf("reading the Claude transcript: %w", err)
		}
	}
}

func isTitleLine(line []byte) bool {
	for _, prefix := range titleLines {
		if bytes.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}
