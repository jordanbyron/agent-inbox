package release

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

// The labels .github/release.yml groups notes by; a mixed pull request takes the first that applies.
var typeLabels = []struct{ prefix, label string }{
	{"feat", "enhancement"},
	{"fix", "bug"},
	{"refactor", "refactor"},
	{"docs", "documentation"},
}

// PullRequestFields is the --json gh pr view needs to fill a PullRequest.
const PullRequestFields = "number,title,labels,commits"

// PullRequest is a pull request merged since the last release.
type PullRequest struct {
	Number         int
	Title          string
	LabelNames     []string
	CommitSubjects []string
}

// ParsePullRequest reads gh pr view's JSON for PullRequestFields.
func ParsePullRequest(data []byte) (PullRequest, error) {
	var raw struct {
		Number  int
		Title   string
		Labels  []struct{ Name string }
		Commits []struct{ MessageHeadline string }
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return PullRequest{}, fmt.Errorf("reading pull request: %w", err)
	}
	pr := PullRequest{Number: raw.Number, Title: raw.Title}
	for _, l := range raw.Labels {
		pr.LabelNames = append(pr.LabelNames, l.Name)
	}
	for _, c := range raw.Commits {
		pr.CommitSubjects = append(pr.CommitSubjects, c.MessageHeadline)
	}
	return pr, nil
}

// MissingLabel is the type label its title's prefix, or failing that its commits', call for and it lacks.
func (pr PullRequest) MissingLabel() string {
	for _, t := range typeLabels {
		if slices.Contains(pr.LabelNames, t.label) {
			return ""
		}
	}
	if label := labelFor(pr.Title); label != "" {
		return label
	}
	for _, t := range typeLabels {
		for _, c := range pr.CommitSubjects {
			if prefixOf(c) == t.prefix {
				return t.label
			}
		}
	}
	return ""
}

func labelFor(title string) string {
	p := prefixOf(title)
	for _, t := range typeLabels {
		if p == t.prefix {
			return t.label
		}
	}
	return ""
}

func prefixOf(line string) string {
	if m := subject.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

var mergeButtonSubject = regexp.MustCompile(`(?m)\A(?:Merge pull request #(\d+) |.*\(#(\d+)\)\s*$)`)

// PullRequestNumbers are the pull requests the commit messages merged or squashed, in order.
func PullRequestNumbers(messages []string) []int {
	var numbers []int
	for _, message := range messages {
		m := mergeButtonSubject.FindStringSubmatch(firstLine(message))
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1] + m[2])
		if err == nil && !slices.Contains(numbers, n) {
			numbers = append(numbers, n)
		}
	}
	return numbers
}
