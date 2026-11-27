// Package slash finds the slash commands a harness offers at its own
// prompt: its skills and commands, read from the directories its recipe
// names. Built-in commands live inside the harness and are not listed;
// typing one still works, since the prompt goes to the harness verbatim.
package slash

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Command is one slash command.
type Command struct {
	Name        string
	Description string
}

// String is the command as typed.
func (c Command) String() string {
	return "/" + c.Name
}

// Where says where to look for commands.
type Where struct {
	// Dirs are the recipe's command directories. A directory named skills
	// holds one skill per subdirectory; any other holds command files.
	Dirs []string
	// Project is the directory the agent will run in. A directory under ~
	// is looked for there too, and the project's wins on a clash.
	Project string
	Home    string
	// Plugins reads Claude Code's installed plugins and synced skills too.
	Plugins bool
}

const syncedPlugin = "anthropic-skills"

// List returns the commands, sorted by name.
func List(w Where) []Command {
	var found []Command
	for _, dir := range w.Dirs {
		rest, underHome := strings.CutPrefix(dir, "~/")
		if underHome && w.Project != "" {
			found = append(found, fromDir(filepath.Join(w.Project, rest), "")...)
		}
		found = append(found, fromDir(expandHome(dir, w.Home), "")...)
	}
	if w.Plugins {
		found = append(found, plugins(w.Project, w.Home)...)
		found = append(found, synced(w.Home)...)
	}
	seen := map[string]bool{}
	var out []Command
	for _, c := range found {
		if !seen[c.Name] {
			seen[c.Name] = true
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Command) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Match returns the commands whose names start with query, then those that
// hold it anywhere else, ignoring case.
func Match(commands []Command, query string) []Command {
	q := strings.ToLower(query)
	var starts, inside []Command
	for _, c := range commands {
		name := strings.ToLower(c.Name)
		switch {
		case strings.HasPrefix(name, q):
			starts = append(starts, c)
		case strings.Contains(name, q):
			inside = append(inside, c)
		}
	}
	return append(starts, inside...)
}

func expandHome(dir, home string) string {
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return dir
}

func fromDir(dir, prefix string) []Command {
	if filepath.Base(dir) == "skills" {
		return skills(dir, prefix)
	}
	return commands(dir, prefix)
}

func pluginRoot(root, prefix string) []Command {
	return append(skills(filepath.Join(root, "skills"), prefix), commands(filepath.Join(root, "commands"), prefix)...)
}

func skills(dir, prefix string) []Command {
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
	slices.Sort(paths)
	var out []Command
	for _, path := range paths {
		meta := frontmatter(path)
		if meta["user-invocable"] == "false" {
			continue
		}
		out = append(out, Command{qualify(prefix, filepath.Base(filepath.Dir(path))), meta["description"]})
	}
	return out
}

// A command file's name is its own; a subdirectory only groups them.
func commands(dir, prefix string) []Command {
	var paths []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	slices.Sort(paths)
	var out []Command
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		out = append(out, Command{qualify(prefix, name), frontmatter(path)["description"]})
	}
	return out
}

func qualify(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + ":" + name
}

// installed_plugins.json maps "name@marketplace" to where the plugin was
// unpacked: one entry, or a list of them, one per install scope.
type installedPlugins struct {
	Plugins map[string]json.RawMessage `json:"plugins"`
}

type install struct {
	InstallPath string `json:"installPath"`
}

func plugins(project, home string) []Command {
	var installed installedPlugins
	readJSON(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), &installed)
	var out []Command
	for _, key := range enabledPlugins(project, home) {
		raw, ok := installed.Plugins[key]
		if !ok {
			continue
		}
		var entries []install
		if json.Unmarshal(raw, &entries) != nil {
			var one install
			if json.Unmarshal(raw, &one) != nil {
				continue
			}
			entries = []install{one}
		}
		name, _, _ := strings.Cut(key, "@")
		var roots []string
		for _, e := range entries {
			if e.InstallPath != "" && !slices.Contains(roots, e.InstallPath) {
				roots = append(roots, e.InstallPath)
				out = append(out, pluginRoot(e.InstallPath, name)...)
			}
		}
	}
	return out
}

// enabledPlugins layers the settings files as Claude Code does, the later
// file winning, and returns the plugins left switched on.
func enabledPlugins(project, home string) []string {
	files := []string{filepath.Join(home, ".claude", "settings.json")}
	if project != "" {
		files = append(files,
			filepath.Join(project, ".claude", "settings.json"),
			filepath.Join(project, ".claude", "settings.local.json"))
	}
	on := map[string]bool{}
	for _, path := range files {
		var settings struct {
			EnabledPlugins map[string]any `json:"enabledPlugins"`
		}
		readJSON(path, &settings)
		for key, value := range settings.EnabledPlugins {
			on[key] = value == true
		}
	}
	var keys []string
	for key, enabled := range on {
		if enabled {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

func synced(home string) []Command {
	buckets, _ := filepath.Glob(filepath.Join(home, ".claude", "skills", "synced", "*"))
	slices.Sort(buckets)
	var out []Command
	for _, bucket := range buckets {
		out = append(out, skills(bucket, syncedPlugin)...)
	}
	return out
}

// A settings file that is missing or broken counts as empty, as it does
// for Claude Code.
func readJSON(path string, v any) {
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, v)
	}
}

var frontmatterKey = regexp.MustCompile(`^([\w-]+):\s*(.*)$`)

const frontmatterLines = 60

// frontmatter reads the YAML front matter's scalar keys without a YAML
// parser: enough for description and user-invocable. A folded or quoted
// description is read up to its first line.
func frontmatter(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	scan := bufio.NewScanner(f)
	if !scan.Scan() || strings.TrimSpace(scan.Text()) != "---" {
		return out
	}
	key := ""
	for i := 1; i < frontmatterLines && scan.Scan(); i++ {
		line := scan.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if m := frontmatterKey.FindStringSubmatch(line); m != nil {
			key = m[1]
			out[key] = unquote(strings.TrimSpace(m[2]))
		} else if key != "" && out[key] == "" && strings.HasPrefix(line, " ") {
			out[key] = unquote(strings.TrimSpace(line))
		}
	}
	return out
}

var blockScalar = regexp.MustCompile(`^[>|][-+]?$`)

func unquote(s string) string {
	s = blockScalar.ReplaceAllString(s, "")
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return strings.ReplaceAll(s, "''", "'")
}
