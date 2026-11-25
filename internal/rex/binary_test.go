package rex_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/rex"
)

func TestFindBinary(t *testing.T) {
	root := t.TempDir()
	dir := func(name string, mode os.FileMode) string {
		d := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(d, 0o755))
		if mode != 0 {
			require.NoError(t, os.WriteFile(filepath.Join(d, "rex"), []byte("#!/bin/sh\n"), mode))
		}
		return d
	}
	binDir := dir("bin-dir", 0o755)
	empty := dir("empty", 0)
	first := dir("first", 0o755)
	second := dir("second", 0o755)
	notExecutable := dir("not-executable", 0o644)
	home := filepath.Join(root, "home")
	staged := filepath.Join(home, "Library", "Application Support", "com.superlogical.rex", "RexServer", "Versions", "Current", "Rex Server.app", "Contents", "MacOS", "rex")
	require.NoError(t, os.MkdirAll(filepath.Dir(staged), 0o755))
	require.NoError(t, os.WriteFile(staged, []byte("#!/bin/sh\n"), 0o755))
	app := filepath.Join(dir("app", 0o755), "rex")
	betaApp := filepath.Join(dir("beta-app", 0o755), "rex")
	missingApp := filepath.Join(empty, "rex")

	cases := []struct {
		name string
		env  map[string]string
		apps []string
		want string
	}{
		{
			name: "REX_BIN_DIR comes before PATH",
			env:  map[string]string{"REX_BIN_DIR": binDir, "PATH": first, "HOME": home},
			apps: []string{app, betaApp},
			want: filepath.Join(binDir, "rex"),
		},
		{
			name: "a REX_BIN_DIR without rex falls through to PATH",
			env:  map[string]string{"REX_BIN_DIR": empty, "PATH": first, "HOME": home},
			apps: []string{app, betaApp},
			want: filepath.Join(first, "rex"),
		},
		{
			name: "PATH is searched in order",
			env:  map[string]string{"PATH": empty + string(os.PathListSeparator) + first + string(os.PathListSeparator) + second, "HOME": home},
			apps: []string{app, betaApp},
			want: filepath.Join(first, "rex"),
		},
		{
			name: "a rex that isn't executable is skipped",
			env:  map[string]string{"PATH": notExecutable + string(os.PathListSeparator) + second},
			apps: []string{app, betaApp},
			want: filepath.Join(second, "rex"),
		},
		{
			name: "the staged server copy under HOME comes before the app bundles",
			env:  map[string]string{"PATH": empty, "HOME": home},
			apps: []string{app, betaApp},
			want: staged,
		},
		{
			name: "the first present app bundle is used when HOME has no staged server",
			env:  map[string]string{"PATH": empty, "HOME": empty},
			apps: []string{app, betaApp},
			want: app,
		},
		{
			name: "falls back to the beta app bundle when Rex.app is absent",
			env:  map[string]string{"PATH": empty, "HOME": empty},
			apps: []string{missingApp, betaApp},
			want: betaApp,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rex.FindBinaryWithApp(func(key string) string { return tc.env[key] }, tc.apps...)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("no rex anywhere is ErrNoBinary", func(t *testing.T) {
		_, err := rex.FindBinaryWithApp(func(string) string { return empty }, missingApp)
		require.ErrorIs(t, err, rex.ErrNoBinary)
	})
}
