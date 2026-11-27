// Command release cuts an agent-inbox release; bin/release runs it. See CONTRIBUTING.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jordanbyron/agent-inbox/internal/release"
)

type shell struct{}

func (shell) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func main() {
	var opts release.Options
	flag.BoolVar(&opts.DryRun, "dry-run", false, "show the version it would release and the labels it would add")
	flag.Parse()
	if err := release.Cut(shell{}, os.Stdout, opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
