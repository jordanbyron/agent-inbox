package release

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The tap Homebrew installs agent-inbox from: brew install jordanbyron/tap/agent-inbox.
const (
	TapRepo     = "jordanbyron/homebrew-tap"
	FormulaPath = "Formula/agent-inbox.rb"
)

// ArchiveURL is the source tarball GitHub serves for tag, which the formula builds.
func ArchiveURL(tag string) string {
	return "https://github.com/jordanbyron/agent-inbox/archive/refs/tags/" + tag + ".tar.gz"
}

var (
	formulaURL    = regexp.MustCompile(`(?m)^(\s*url )"[^"]*"$`)
	formulaSHA256 = regexp.MustCompile(`(?m)^(\s*sha256 )"[^"]*"$`)
)

// BumpFormula points the formula's url and sha256 at a new release.
func BumpFormula(formula, url, sha256 string) (string, error) {
	if len(formulaURL.FindAllString(formula, -1)) != 1 || len(formulaSHA256.FindAllString(formula, -1)) != 1 {
		return "", fmt.Errorf("%s needs exactly one url and one sha256 line", FormulaPath)
	}
	formula = formulaURL.ReplaceAllString(formula, `${1}"`+url+`"`)
	return formulaSHA256.ReplaceAllString(formula, `${1}"`+sha256+`"`), nil
}

// UpdateTap pushes a commit to the tap that moves the formula to tag.
func UpdateTap(sh Shell, out io.Writer, tag string) error {
	dir, err := os.MkdirTemp("", "agent-inbox-tap")
	if err != nil {
		return fmt.Errorf("making a directory for the tap: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	tarball := filepath.Join(dir, "source.tar.gz")
	if _, err := sh.Run("curl", "--fail", "--silent", "--show-error", "--location", "--output", tarball, ArchiveURL(tag)); err != nil {
		return err
	}
	sum, err := sh.Run("shasum", "--algorithm", "256", tarball)
	if err != nil {
		return err
	}
	sha256, _, _ := strings.Cut(sum, " ")

	tap := filepath.Join(dir, "tap")
	if _, err := sh.Run("gh", "repo", "clone", TapRepo, tap, "--", "--quiet", "--depth=1"); err != nil {
		return err
	}
	path := filepath.Join(tap, FormulaPath)
	formula, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading the formula: %w", err)
	}
	bumped, err := BumpFormula(string(formula), ArchiveURL(tag), sha256)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(bumped), 0o644); err != nil {
		return fmt.Errorf("writing the formula: %w", err)
	}
	for _, args := range [][]string{
		{"-C", tap, "commit", "--quiet", "--all", "--message", "agent-inbox " + tag},
		{"-C", tap, "push", "--quiet"},
	} {
		if _, err := sh.Run("git", args...); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(out, "%s now installs %s\n", TapRepo, tag)
	return err
}
