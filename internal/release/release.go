package release

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Shell runs a command and returns its trimmed standard output.
type Shell interface {
	Run(name string, args ...string) (string, error)
}

// Options are the flags bin/release takes.
type Options struct {
	DryRun     bool
	FormulaTag string
}

// Cut releases main's HEAD as the next tag, or with DryRun says what it would do.
func Cut(sh Shell, out io.Writer, opts Options) error {
	if opts.FormulaTag != "" {
		return UpdateTap(sh, out, opts.FormulaTag)
	}
	if _, err := sh.Run("git", "fetch", "--quiet", "--tags", "origin"); err != nil {
		return err
	}
	if !opts.DryRun {
		if err := checkReleasable(sh); err != nil {
			return err
		}
	}

	tags, err := sh.Run("git", "tag", "--list", "v*", "--sort=-v:refname")
	if err != nil {
		return err
	}
	last, _, _ := strings.Cut(tags, "\n")
	rangeSpec := "HEAD"
	if last != "" {
		rangeSpec = last + "..HEAD"
	}
	log, err := sh.Run("git", "log", rangeSpec, "--format=%B%x00")
	if err != nil {
		return err
	}
	messages := strings.Split(log, "\x00")

	tag, reason := FirstVersion, "the first release"
	if last != "" {
		bump := BumpFor(last, messages)
		if bump == None {
			_, err := fmt.Fprintf(out, "nothing to release since %s\n", last)
			return err
		}
		if tag, err = Next(last, bump); err != nil {
			return err
		}
		reason = fmt.Sprintf("%s bump over %s", bump, last)
	}

	if err := labelPullRequests(sh, out, messages, opts.DryRun); err != nil {
		return err
	}
	if opts.DryRun {
		_, err := fmt.Fprintf(out, "would release %s: %s, and move %s to it\n", tag, reason, TapRepo)
		return err
	}
	if err := publish(sh, out, tag); err != nil {
		return err
	}
	if err := UpdateTap(sh, out, tag); err != nil {
		return fmt.Errorf("%s is released but brew still installs the last one; finish with bin/release --formula %s: %w", tag, tag, err)
	}
	return nil
}

func checkReleasable(sh Shell) error {
	branch, err := sh.Run("git", "branch", "--show-current")
	if err != nil {
		return err
	}
	if branch != "main" {
		return fmt.Errorf("release from main, not %s", branch)
	}
	if status, err := sh.Run("git", "status", "--porcelain"); err != nil || status != "" {
		return errors.Join(err, errors.New("commit or discard your changes first: the working tree is dirty"))
	}
	head, err := sh.Run("git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if remote, err := sh.Run("git", "rev-parse", "origin/main"); err != nil || remote != head {
		return errors.Join(err, errors.New("pull first: HEAD is not origin/main"))
	}
	return nil
}

func labelPullRequests(sh Shell, out io.Writer, messages []string, dryRun bool) error {
	for _, number := range PullRequestNumbers(messages) {
		n := strconv.Itoa(number)
		data, err := sh.Run("gh", "pr", "view", n, "--json", PullRequestFields)
		if err != nil {
			return err
		}
		pr, err := ParsePullRequest([]byte(data))
		if err != nil {
			return err
		}
		label := pr.MissingLabel()
		switch {
		case label == "":
		case dryRun:
			if _, err := fmt.Fprintf(out, "would label #%d %s\n", number, label); err != nil {
				return err
			}
		default:
			if _, err := sh.Run("gh", "pr", "edit", n, "--add-label", label); err != nil {
				return err
			}
		}
	}
	return nil
}

func publish(sh Shell, out io.Writer, tag string) error {
	if _, err := sh.Run("git", "tag", "--annotate", tag, "--message", tag); err != nil {
		return err
	}
	if _, err := sh.Run("git", "push", "--quiet", "origin", tag); err != nil {
		_, _ = sh.Run("git", "tag", "--delete", tag)
		return fmt.Errorf("pushing %s, so the local tag is deleted again: %w", tag, err)
	}
	url, err := sh.Run("gh", "release", "create", tag, "--generate-notes", "--verify-tag")
	if err != nil {
		return fmt.Errorf("%s is pushed but has no release; finish with gh release create %s --generate-notes --verify-tag: %w", tag, tag, err)
	}
	_, err = fmt.Fprintln(out, url)
	return err
}
