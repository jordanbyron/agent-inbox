package images_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/images"
)

type fakeClipboard struct {
	image   []byte
	text    string
	err     error
	savedTo string
}

func (c *fakeClipboard) SaveImage(path string) (bool, error) {
	if c.err != nil || c.image == nil {
		return false, c.err
	}
	c.savedTo = path
	return true, os.WriteFile(path, c.image, 0o600)
}

func (c *fakeClipboard) Text() (string, error) {
	return c.text, nil
}

func touch(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestDropped(t *testing.T) {
	dir := t.TempDir()
	shot := filepath.Join(dir, "Screen Shot.PNG")
	notes := filepath.Join(dir, "notes.md")
	touch(t, shot, time.Now())
	touch(t, notes, time.Now())
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"unescapes a path the terminal escaped", dir + "/Screen\\ Shot.PNG ", []string{shot}},
		{"takes a quoted path", "'" + shot + "'", []string{shot}},
		{"takes a double-quoted path", `"` + shot + `"`, []string{shot}},
		{"takes any file, not only an image", notes + " ", []string{notes}},
		{"takes several files dropped at once", dir + "/Screen\\ Shot.PNG " + notes, []string{shot, notes}},
		{"leaves a missing file as text", dir + "/missing.png", nil},
		{"leaves a directory as text", dir, nil},
		{"leaves words as text", "look at this", nil},
		{"leaves a file among words as text", "see " + notes, nil},
		{"leaves an open quote as text", "'" + shot, nil},
		{"leaves nothing as text", "  ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := images.Dropped(tc.text)
			require.Equal(t, tc.want != nil, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestIsImage(t *testing.T) {
	for path, want := range map[string]bool{
		"a.png": true, "B.JPG": true, "c.jpeg": true, "d.gif": true, "e.webp": true,
		"f.bmp": true, "g.svg": true, "notes.md": false, "png": false,
	} {
		require.Equal(t, want, images.IsImage(path), path)
	}
}

func TestFromClipboard(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 30, 15, 123_000_000, time.UTC)

	t.Run("saves an image to a file of its own and prunes stale ones", func(t *testing.T) {
		dir := t.TempDir()
		old, kept, notes := filepath.Join(dir, "old.jpg"), filepath.Join(dir, "kept.webp"), filepath.Join(dir, "notes.txt")
		touch(t, old, now.Add(-images.KeepFor-time.Second))
		touch(t, kept, now.Add(-time.Minute))
		touch(t, notes, now.Add(-15*24*time.Hour))
		clip := &fakeClipboard{image: []byte("png")}
		got, err := images.FromClipboard(clip, dir, now)
		require.NoError(t, err)
		require.Equal(t, images.Clip{Image: filepath.Join(dir, "20261123-093015.123.png")}, got)
		require.Equal(t, got.Image, clip.savedTo)
		require.NoFileExists(t, old)
		require.FileExists(t, kept)
		require.FileExists(t, notes)
	})

	t.Run("makes the directory it saves into", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "images")
		got, err := images.FromClipboard(&fakeClipboard{image: []byte("png")}, dir, now)
		require.NoError(t, err)
		require.FileExists(t, got.Image)
	})

	t.Run("falls back to the clipboard's text", func(t *testing.T) {
		got, err := images.FromClipboard(&fakeClipboard{text: "hi\n"}, t.TempDir(), now)
		require.NoError(t, err)
		require.Equal(t, images.Clip{Text: "hi\n"}, got)
	})

	t.Run("holds nothing when the clipboard is empty", func(t *testing.T) {
		got, err := images.FromClipboard(&fakeClipboard{}, t.TempDir(), now)
		require.NoError(t, err)
		require.Equal(t, images.Clip{}, got)
	})

	t.Run("says why it could not read the clipboard", func(t *testing.T) {
		_, err := images.FromClipboard(&fakeClipboard{err: errors.New("denied")}, t.TempDir(), now)
		require.ErrorContains(t, err, "reading the clipboard's image: denied")
	})
}
