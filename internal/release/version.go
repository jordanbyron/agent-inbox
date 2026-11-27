// Package release tags and publishes a semver release from the conventional commits since the last tag.
package release

import (
	"fmt"
	"regexp"
	"strings"
)

// Bump is how far a release moves the version.
type Bump int

// Rising order: BumpFor takes the max.
const (
	None Bump = iota
	Patch
	Minor
	Major
)

func (b Bump) String() string {
	return [...]string{"no", "patch", "minor", "major"}[b]
}

// FirstVersion is the tag of a repository's first release.
const FirstVersion = "v0.1.0"

var (
	subject          = regexp.MustCompile(`\A(\w+)(?:\([^)]*\))?(!)?:`)
	breakingFooter   = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE:`)
	squashBodyBullet = regexp.MustCompile(`(?m)^\* .*$`)
)

// BumpFor is the largest bump the conventional commit messages call for over current.
func BumpFor(current string, messages []string) Bump {
	bump := None
	for _, message := range messages {
		bump = max(bump, kind(message))
	}
	// Semver's 0.x rule: nothing is stable yet, so breaking changes bump minor.
	if bump == Major && strings.HasPrefix(current, "v0.") {
		return Minor
	}
	return bump
}

func kind(message string) Bump {
	subjects := []string{firstLine(message)}
	if !subject.MatchString(subjects[0]) {
		subjects = squashBodyBullet.FindAllString(message, -1)
	}
	bump, conventional := None, false
	for _, s := range subjects {
		m := subject.FindStringSubmatch(strings.TrimPrefix(s, "* "))
		if m == nil {
			continue
		}
		conventional = true
		switch {
		case m[2] == "!":
			bump = Major
		case m[1] == "feat":
			bump = max(bump, Minor)
		case m[1] == "fix":
			bump = max(bump, Patch)
		}
	}
	if conventional && breakingFooter.MatchString(message) {
		return Major
	}
	return bump
}

// Next is the vX.Y.Z tag current becomes after bump.
func Next(current string, bump Bump) (string, error) {
	var major, minor, patch int
	if _, err := fmt.Sscanf(current, "v%d.%d.%d", &major, &minor, &patch); err != nil {
		return "", fmt.Errorf("reading tag %q: %w", current, err)
	}
	if want := fmt.Sprintf("v%d.%d.%d", major, minor, patch); want != current {
		return "", fmt.Errorf("tag %q is not a plain vX.Y.Z", current)
	}
	switch bump {
	case Major:
		return fmt.Sprintf("v%d.0.0", major+1), nil
	case Minor:
		return fmt.Sprintf("v%d.%d.0", major, minor+1), nil
	case Patch:
		return fmt.Sprintf("v%d.%d.%d", major, minor, patch+1), nil
	default:
		return current, nil
	}
}

func firstLine(message string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	return line
}
