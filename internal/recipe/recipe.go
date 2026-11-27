// Package recipe holds what agent-inbox knows about each harness: how to
// start and resume it, and what its new-agent form offers.
package recipe

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Recipe is what agent-inbox knows about one harness.
type Recipe struct {
	// Name is the recipe's key under [recipes].
	Name string `toml:"-"`
	// App is the `app` the harness reports in its Rex status record.
	App string `toml:"app"`
	// Start is the command that starts a new agent.
	Start []string `toml:"start"`
	// Resume is the command that resumes a settled agent.
	Resume []string `toml:"resume"`
	// ResumeLatest resumes the directory's latest conversation, for an
	// agent whose session id was never learned.
	ResumeLatest []string `toml:"-"`
	// Mention is how a prompt mentions a file, with {path} filled in.
	Mention string `toml:"mention"`
	// Commands are the directories that hold the harness's slash commands.
	Commands []string `toml:"commands"`
	// Choices are what the form offers for Model, Effort and Permissions.
	Choices Choices `toml:"choices"`
	// PermissionArgs maps a permissions choice to the arguments it stands
	// for, for a harness that needs more than one flag per choice. When it
	// is set they take the place of Start's {permissions} argument and the
	// flag before it; a choice it doesn't list adds nothing.
	PermissionArgs map[string][]string `toml:"permission_args"`
	// Sessions learns the session id of a harness that cannot be given one
	// at start. It is nil for a recipe from the config file.
	Sessions SessionFinder `toml:"-"`
	// Titles reads the title the harness gives a conversation. It is nil
	// for a recipe from the config file.
	Titles TitleFinder `toml:"-"`
}

// Choices are the values the form offers for each of its pickers.
type Choices struct {
	Model       []string `toml:"model"`
	Effort      []string `toml:"effort"`
	Permissions []string `toml:"permissions"`
}

// Values fill a recipe's placeholders.
type Values struct {
	SessionID   string
	Name        string
	Model       string
	Effort      string
	Permissions string
	Prompt      string
	Dir         string
}

// StartArgs returns the argv that starts an agent.
func (r Recipe) StartArgs(v Values) []string {
	return render(r.withPermissions(r.Start, v.Permissions), v)
}

func (r Recipe) withPermissions(template []string, choice string) []string {
	i := slices.IndexFunc(template, func(arg string) bool { return strings.Contains(arg, "{permissions}") })
	if r.PermissionArgs == nil || i < 0 {
		return template
	}
	from := i
	if i > 0 && isFlag(template[i-1]) && !strings.HasPrefix(template[i], "-") {
		from = i - 1
	}
	return slices.Concat(template[:from], r.PermissionArgs[choice], template[i+1:])
}

func isFlag(arg string) bool {
	return strings.HasPrefix(arg, "-") && !placeholder.MatchString(arg)
}

// ResumeArgs returns the argv that resumes an agent, falling back to
// ResumeLatest when v has no session id and the recipe has one.
func (r Recipe) ResumeArgs(v Values) []string {
	if v.SessionID == "" && r.ResumeLatest != nil {
		return render(r.ResumeLatest, v)
	}
	return render(r.Resume, v)
}

// TakesSessionID reports whether Start passes a session id, which the
// caller then makes with NewSessionID.
func (r Recipe) TakesSessionID() bool {
	return slices.ContainsFunc(r.Start, func(arg string) bool {
		return strings.Contains(arg, "{session_id}")
	})
}

// FindSessionID asks the recipe's SessionFinder for the agent's session id.
// It reports false when there is none to ask or it found nothing.
func (r Recipe) FindSessionID(ctx context.Context, q SessionQuery) (string, bool, error) {
	if r.Sessions == nil {
		return "", false, nil
	}
	return r.Sessions.FindSessionID(ctx, q)
}

var placeholder = regexp.MustCompile(`\{[^{}]*\}`)

func (v Values) fields() map[string]string {
	return map[string]string{
		"{session_id}":  v.SessionID,
		"{name}":        v.Name,
		"{model}":       v.Model,
		"{effort}":      v.Effort,
		"{permissions}": v.Permissions,
		"{prompt}":      v.Prompt,
		"{dir}":         v.Dir,
	}
}

func isChoice(p string) bool {
	return p == "{model}" || p == "{effort}" || p == "{permissions}"
}

// render fills in template. An argument whose placeholder is empty, or is a
// choice set to "default", is left out, and so is the flag before it unless
// the argument is a flag itself (`--model={model}`).
func render(template []string, v Values) []string {
	fields := v.fields()
	var out []string
	lastIsFlag := false
	for _, arg := range template {
		value, keep := fill(arg, fields)
		if !keep {
			if lastIsFlag && !strings.HasPrefix(arg, "-") {
				out = out[:len(out)-1]
			}
			lastIsFlag = false
			continue
		}
		out = append(out, value)
		lastIsFlag = isFlag(arg)
	}
	return out
}

func fill(arg string, fields map[string]string) (string, bool) {
	keep := true
	value := placeholder.ReplaceAllStringFunc(arg, func(p string) string {
		value := fields[p]
		if value == "" || (isChoice(p) && value == "default") {
			keep = false
		}
		return value
	})
	return value, keep
}

func (r Recipe) check() error {
	if r.App == "" {
		return fmt.Errorf("recipes.%s.app: is empty", r.Name)
	}
	if len(r.Start) == 0 {
		return fmt.Errorf("recipes.%s.start: is empty", r.Name)
	}
	if len(r.Resume) == 0 {
		return fmt.Errorf("recipes.%s.resume: is empty", r.Name)
	}
	known := slices.Collect(maps.Keys(Values{}.fields()))
	for key, template := range map[string][]string{"start": r.Start, "resume": r.Resume} {
		for _, arg := range template {
			if err := checkPlaceholders(arg, known); err != nil {
				return fmt.Errorf("recipes.%s.%s: %w", r.Name, key, err)
			}
		}
	}
	if err := checkPlaceholders(r.Mention, []string{"{path}"}); err != nil {
		return fmt.Errorf("recipes.%s.mention: %w", r.Name, err)
	}
	for choice, args := range r.PermissionArgs {
		for _, arg := range args {
			if err := checkPlaceholders(arg, nil); err != nil {
				return fmt.Errorf("recipes.%s.permission_args.%s: %w", r.Name, choice, err)
			}
		}
	}
	return nil
}

func checkPlaceholders(s string, known []string) error {
	for _, p := range placeholder.FindAllString(s, -1) {
		if !slices.Contains(known, p) {
			return fmt.Errorf("unknown placeholder %s", p)
		}
	}
	return nil
}

// Set is the recipes agent-inbox knows, built-ins first.
type Set []Recipe

// WithBuiltins checks the recipes read from the config file and returns
// them with the built-ins. One with a built-in's name replaces it; the
// rest follow in name order.
func WithBuiltins(configured map[string]Recipe) (Set, error) {
	set := Builtins()
	for _, name := range slices.Sorted(maps.Keys(configured)) {
		r := configured[name]
		r.Name = name
		if err := r.check(); err != nil {
			return nil, err
		}
		if i := set.index(name); i >= 0 {
			set[i] = r
		} else {
			set = append(set, r)
		}
	}
	return set, nil
}

// Named returns the recipe with the given name.
func (s Set) Named(name string) (Recipe, bool) {
	if i := s.index(name); i >= 0 {
		return s[i], true
	}
	return Recipe{}, false
}

// ForApp returns the recipe for the app a Rex status record names. When
// two recipes share an app, the first wins.
func (s Set) ForApp(app string) (Recipe, bool) {
	for _, r := range s {
		if r.App == app {
			return r, true
		}
	}
	return Recipe{}, false
}

func (s Set) index(name string) int {
	return slices.IndexFunc(s, func(r Recipe) bool { return r.Name == name })
}
