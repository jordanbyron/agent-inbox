package release_test

import (
	"bytes"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jordanbyron/agent-inbox/internal/release"
)

type fakeShell struct {
	replies map[string]string
	fails   map[string]bool
	ran     []string
}

func (f *fakeShell) Run(name string, args ...string) (string, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.ran = append(f.ran, line)
	if f.fails[line] {
		return "", errors.New("failed")
	}
	return f.replies[line], nil
}

func releasable(lastTag, log string) *fakeShell {
	sh := &fakeShell{
		replies: map[string]string{
			"git branch --show-current":                              "main",
			"git rev-parse HEAD":                                     "abc",
			"git rev-parse origin/main":                              "abc",
			"git tag --list v* --sort=-v:refname":                    lastTag,
			"gh pr view 7 --json number,title,labels,commits":        `{"number":7,"title":"feat: a"}`,
			"gh release create v0.2.0 --generate-notes --verify-tag": "https://example/releases/v0.2.0",
		},
		fails: map[string]bool{},
	}
	rangeSpec := "HEAD"
	if lastTag != "" {
		rangeSpec = lastTag + "..HEAD"
	}
	sh.replies["git log "+rangeSpec+" --format=%B%x00"] = log
	return sh
}

const featLog = "Merge pull request #7 from x/y\n\nfeat: a\x00\nfeat: a\n\x00"

func TestCutReleasesTheNextTag(t *testing.T) {
	sh := releasable("v0.1.0", featLog)
	var out bytes.Buffer

	require.NoError(t, release.Cut(sh, &out, release.Options{}))

	require.Contains(t, sh.ran, "gh pr edit 7 --add-label enhancement")
	require.Contains(t, sh.ran, "git tag --annotate v0.2.0 --message v0.2.0")
	require.Contains(t, sh.ran, "git push --quiet origin v0.2.0")
	require.Equal(t, "https://example/releases/v0.2.0\n", out.String())
}

func TestCutDryRunChangesNothing(t *testing.T) {
	sh := releasable("v0.1.0", featLog)
	sh.replies["git branch --show-current"] = "some-branch"
	var out bytes.Buffer

	require.NoError(t, release.Cut(sh, &out, release.Options{DryRun: true}))

	require.Equal(t, "would label #7 enhancement\nwould release v0.2.0: minor bump over v0.1.0\n", out.String())
	for _, line := range sh.ran {
		require.NotRegexp(t, `^(git tag --annotate|git push|gh pr edit|gh release)`, line)
	}
}

func TestCutFirstRelease(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, release.Cut(releasable("", "docs: a\x00"), &out, release.Options{DryRun: true}))
	require.Equal(t, "would release v0.1.0: the first release\n", out.String())
}

func TestCutWithNothingToRelease(t *testing.T) {
	sh := releasable("v0.1.0", "docs: a\x00\nrefactor: b\x00")
	var out bytes.Buffer

	require.NoError(t, release.Cut(sh, &out, release.Options{}))

	require.Equal(t, "nothing to release since v0.1.0\n", out.String())
	require.NotContains(t, sh.ran, "git tag --annotate v0.1.1 --message v0.1.1")
}

func TestCutRefuses(t *testing.T) {
	cases := []struct {
		name  string
		reply map[string]string
		want  string
	}{
		{name: "off main", reply: map[string]string{"git branch --show-current": "topic"}, want: "release from main, not topic"},
		{name: "dirty", reply: map[string]string{"git status --porcelain": " M go.mod"}, want: "working tree is dirty"},
		{name: "behind", reply: map[string]string{"git rev-parse origin/main": "def"}, want: "HEAD is not origin/main"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sh := releasable("v0.1.0", featLog)
			maps.Copy(sh.replies, c.reply)
			require.ErrorContains(t, release.Cut(sh, &bytes.Buffer{}, release.Options{}), c.want)
			require.NotContains(t, sh.ran, "git tag --annotate v0.2.0 --message v0.2.0")
		})
	}
}

func TestCutDeletesTheTagWhenThePushFails(t *testing.T) {
	sh := releasable("v0.1.0", featLog)
	sh.fails["git push --quiet origin v0.2.0"] = true

	require.Error(t, release.Cut(sh, &bytes.Buffer{}, release.Options{}))

	require.Contains(t, sh.ran, "git tag --delete v0.2.0")
	require.NotContains(t, sh.ran, "gh release create v0.2.0 --generate-notes --verify-tag")
}
