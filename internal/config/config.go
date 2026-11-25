// Package config reads agent-inbox's config file, config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/BurntSushi/toml"

	"github.com/jordanbyron/agent-inbox/internal/recipe"
)

// Config is everything config.toml can set, with the defaults filled in.
type Config struct {
	// Sort is "default" or "activity".
	Sort string `toml:"sort"`
	// FoldWorking folds agents at work into the Working section.
	FoldWorking bool `toml:"fold_working"`
	// Notify sends a desktop notification when a family starts needing
	// you or finishes.
	Notify bool `toml:"notify"`
	// Reap deletes families settled for 14 days.
	Reap bool `toml:"reap"`
	// Worktree is how agent-inbox makes an agent's worktree.
	Worktree Worktree `toml:"worktree"`
	// Recipes are the built-in recipes with the config file's own laid
	// over them.
	Recipes recipe.Set `toml:"-"`
}

// Worktree is the [worktree] table.
type Worktree struct {
	// Default is whether the form starts with Worktree on in a git repository.
	Default bool `toml:"default"`
	// Path is the worktree's path, with {repo} and {name} filled in.
	Path string `toml:"path"`
	// Branch is the new branch's name, with {name} filled in.
	Branch string `toml:"branch"`
	// Base is "fresh" to branch from origin's default branch after a
	// fetch, or "head" to branch from the checkout's HEAD.
	Base string `toml:"base"`
}

// The values Worktree.Base can take.
const (
	BaseFresh = "fresh"
	BaseHead  = "head"
)

// Default returns the config used when the file sets nothing.
func Default() Config {
	return Config{
		Sort: "default",
		Worktree: Worktree{
			Default: true,
			Path:    "{repo}/.worktrees/{name}",
			Branch:  "{name}",
			Base:    BaseFresh,
		},
		Recipes: recipe.Builtins(),
	}
}

// Path returns where the config file is: under $XDG_CONFIG_HOME when that
// is an absolute path, else under ~/.config.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	// The XDG spec says to ignore a relative path here.
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding the config file: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "agent-inbox", "config.toml"), nil
}

type file struct {
	Config
	Recipes map[string]recipe.Recipe `toml:"recipes"`
}

// Load reads the config file at path. A missing file is the defaults. An
// unknown key or a bad value is an error that names the file and the key.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	f := file{Config: Default()}
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		return Config{}, fmt.Errorf("reading %s: unknown key %s", path, unknown[0])
	}
	if err := f.check(); err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	cfg := f.Config
	cfg.Recipes, err = recipe.WithBuiltins(f.Recipes)
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return cfg, nil
}

func (f *file) check() error {
	if err := oneOf("sort", f.Sort, "default", "activity"); err != nil {
		return err
	}
	if err := oneOf("worktree.base", f.Worktree.Base, BaseFresh, BaseHead); err != nil {
		return err
	}
	if err := checkTemplate("worktree.path", f.Worktree.Path, "{repo}", "{name}"); err != nil {
		return err
	}
	return checkTemplate("worktree.branch", f.Worktree.Branch, "{name}")
}

func oneOf(key, value string, allowed ...string) error {
	if !slices.Contains(allowed, value) {
		return fmt.Errorf("%s: %q is not one of %q", key, value, allowed)
	}
	return nil
}

var placeholder = regexp.MustCompile(`\{[^{}]*\}`)

func checkTemplate(key, template string, allowed ...string) error {
	if template == "" {
		return fmt.Errorf("%s: is empty", key)
	}
	for _, p := range placeholder.FindAllString(template, -1) {
		if !slices.Contains(allowed, p) {
			return fmt.Errorf("%s: unknown placeholder %s", key, p)
		}
	}
	return nil
}
