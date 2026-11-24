package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/config"
	"github.com/jordanbyron/agent-inbox/internal/form"
	"github.com/jordanbyron/agent-inbox/internal/images"
	"github.com/jordanbyron/agent-inbox/internal/worktree"
)

func newFormCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "form",
		Short:  "Show the new-agent form and print what was chosen as JSON",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := formOptions(cmd)
			if err != nil {
				return err
			}
			result, ok, err := form.Run(cmd.Context(), opts)
			if err != nil || !ok {
				return err
			}
			out := json.NewEncoder(cmd.OutOrStdout())
			out.SetIndent("", "  ")
			if err := out.Encode(result); err != nil {
				return fmt.Errorf("printing the result: %w", err)
			}
			return nil
		},
	}
}

func formOptions(cmd *cobra.Command) (form.Options, error) {
	path, err := config.Path()
	if err != nil {
		return form.Options{}, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return form.Options{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return form.Options{}, fmt.Errorf("finding the home directory: %w", err)
	}
	dir, err := os.Getwd()
	if err != nil {
		return form.Options{}, fmt.Errorf("finding the current directory: %w", err)
	}
	return form.Options{
		Recipes:         cfg.Recipes,
		Dir:             dir,
		WorktreeDefault: cfg.Worktree.Default,
		InRepo: func(dir string) bool {
			return worktree.InRepo(cmd.Context(), worktree.ExecGit{}, dir)
		},
		Home:      home,
		Now:       time.Now,
		Clipboard: images.Pasteboard{},
		ImageDir:  filepath.Join(filepath.Dir(path), "images"),
		// The JSON goes to stdout, so it can be piped while the form shows.
		Output: cmd.ErrOrStderr(),
	}, nil
}
