// Package images brings files into a prompt: a pasted image saved from the
// clipboard, and files dropped on the terminal.
package images

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// KeepFor is how long a saved image is kept. Nothing else knows when the
// agent that read it stopped needing it, so old ones are pruned on the way in.
const KeepFor = 14 * 24 * time.Hour

var extensions = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg"}

// IsImage reports whether path has one of the extensions Claude Code
// attaches as an image.
func IsImage(path string) bool {
	return slices.Contains(extensions, strings.ToLower(filepath.Ext(path)))
}

// Clipboard reads the system clipboard.
type Clipboard interface {
	// SaveImage writes the clipboard's image to path as a PNG, and reports
	// false when the clipboard holds no image.
	SaveImage(path string) (bool, error)
	// Text returns the clipboard's text, or "" when it holds none.
	Text() (string, error)
}

// Clip is what the clipboard held: the path an image was saved to, or text.
type Clip struct {
	Image string
	Text  string
}

// FromClipboard saves the clipboard's image into dir, or failing that
// returns its text. An empty Clip means the clipboard held neither.
func FromClipboard(clip Clipboard, dir string, now time.Time) (Clip, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Clip{}, fmt.Errorf("making the image directory: %w", err)
	}
	prune(dir, now)
	path := filepath.Join(dir, now.Format("20060102-150405.000")+".png")
	saved, err := clip.SaveImage(path)
	if err != nil {
		return Clip{}, fmt.Errorf("reading the clipboard's image: %w", err)
	}
	if saved {
		return Clip{Image: path}, nil
	}
	text, err := clip.Text()
	if err != nil {
		return Clip{}, fmt.Errorf("reading the clipboard's text: %w", err)
	}
	return Clip{Text: text}, nil
}

func prune(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || !IsImage(e.Name()) || now.Sub(info.ModTime()) <= KeepFor {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}

// Dropped returns the files a terminal pasted when they were dropped on it:
// their paths, escaped or quoted the way a shell wants them. It reports
// false unless every word of text names a file.
func Dropped(text string) ([]string, bool) {
	words, ok := shellWords(strings.TrimSpace(text))
	if !ok || len(words) == 0 {
		return nil, false
	}
	for _, path := range words {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, false
		}
	}
	return words, true
}

func shellWords(text string) ([]string, bool) {
	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, r := range text {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '\\':
			escaped, inWord = true, true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, true
}

// Pasteboard is the macOS clipboard. osascript ships with the OS, and PNG
// is the flavor a screenshot puts there. Elsewhere it is always empty.
type Pasteboard struct{}

// The form reads the clipboard while it waits on a key, so a stuck read
// must give up rather than freeze it.
const readTimeout = 5 * time.Second

const clipboardPNG = `on run argv
  set png to the clipboard as «class PNGf»
  set f to open for access POSIX file (item 1 of argv) with write permission
  write png to f
  close access f
end run`

// SaveImage writes the clipboard's PNG to path. osascript fails when the
// clipboard holds no image, which is the false this reports.
func (Pasteboard) SaveImage(path string) (bool, error) {
	osascript, err := exec.LookPath("osascript")
	if err != nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	err = exec.CommandContext(ctx, osascript, "-e", clipboardPNG, path).Run()
	if _, failed := errors.AsType[*exec.ExitError](err); failed {
		_ = os.Remove(path)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("running osascript: %w", err)
	}
	return true, nil
}

// Text returns what pbpaste prints.
func (Pasteboard) Text() (string, error) {
	pbpaste, err := exec.LookPath("pbpaste")
	if err != nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, pbpaste).Output()
	if err != nil {
		return "", fmt.Errorf("running pbpaste: %w", err)
	}
	return string(out), nil
}
