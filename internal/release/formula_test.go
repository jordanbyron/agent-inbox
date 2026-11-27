package release_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/release"
)

const formula = `class AgentInbox < Formula
  desc "Triage inbox for coding agents running in Rex"
  url "https://github.com/jordanbyron/agent-inbox/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "aaa"
  depends_on "go" => :build
end
`

type fakeTap struct {
	t      *testing.T
	pushed string
}

func (f *fakeTap) run(name string, args []string) (string, bool) {
	switch {
	case name == "shasum":
		return "bbb  " + args[len(args)-1], true
	case name == "gh" && slices.Contains(args, "clone"):
		dir := args[3]
		require.NoError(f.t, os.MkdirAll(filepath.Join(dir, "Formula"), 0o755))
		require.NoError(f.t, os.WriteFile(filepath.Join(dir, release.FormulaPath), []byte(formula), 0o600))
		return "", true
	case name == "git" && args[0] == "-C" && slices.Contains(args, "push"):
		data, err := os.ReadFile(filepath.Join(args[1], release.FormulaPath))
		require.NoError(f.t, err)
		f.pushed = string(data)
		return "", true
	}
	return "", false
}

func TestBumpFormula(t *testing.T) {
	got, err := release.BumpFormula(formula, release.ArchiveURL("v0.2.0"), "bbb")
	require.NoError(t, err)
	require.Equal(t, strings.NewReplacer("v0.1.0", "v0.2.0", `"aaa"`, `"bbb"`).Replace(formula), got)
}

func TestBumpFormulaRefusesAnUnexpectedFormula(t *testing.T) {
	cases := map[string]string{
		"no sha256": strings.ReplaceAll(formula, `  sha256 "aaa"`+"\n", ""),
		"two urls":  formula + `  url "https://example/other.tar.gz"` + "\n",
		"no url":    strings.ReplaceAll(formula, "  url ", "  uri "),
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := release.BumpFormula(f, "u", "s")
			require.Error(t, err)
		})
	}
}

func TestUpdateTap(t *testing.T) {
	tap := &fakeTap{t: t}
	sh := &fakeShell{tap: tap.run}
	var out bytes.Buffer

	require.NoError(t, release.UpdateTap(sh, &out, "v0.2.0"))

	require.Contains(t, tap.pushed, `url "https://github.com/jordanbyron/agent-inbox/archive/refs/tags/v0.2.0.tar.gz"`)
	require.Contains(t, tap.pushed, `sha256 "bbb"`)
	require.Equal(t, "jordanbyron/homebrew-tap now installs v0.2.0\n", out.String())
}

func TestCutOnlyMovesTheFormula(t *testing.T) {
	tap := &fakeTap{t: t}
	sh := &fakeShell{tap: tap.run}

	require.NoError(t, release.Cut(sh, &bytes.Buffer{}, release.Options{FormulaTag: "v0.1.1"}))

	require.Contains(t, tap.pushed, "v0.1.1.tar.gz")
	for _, line := range sh.ran {
		require.NotRegexp(t, `^(git fetch|git tag|gh release)`, line)
	}
}

func TestCutSaysHowToFinishWhenTheTapFails(t *testing.T) {
	sh := releasable(t, "v0.1.0", featLog)
	sh.tap = nil

	err := release.Cut(sh, &bytes.Buffer{}, release.Options{})

	require.ErrorContains(t, err, "v0.2.0 is released")
	require.ErrorContains(t, err, "bin/release --formula v0.2.0")
}
