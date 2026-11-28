package list

import (
	"slices"
	"strings"
	"time"
)

type action int

const (
	actionNone action = iota
	actionDown
	actionUp
	actionTop
	actionBottom
	actionHalfPageDown
	actionHalfPageUp
	actionPageDown
	actionPageUp
	actionNextSection
	actionPrevSection
	actionActivate
	actionFoldOpen
	actionFoldClose
	actionFoldToggle
	actionNew
	actionTogglePin
	actionSnooze
	actionWake
	actionSettle
	actionAlias
	actionOpenPullRequest
	actionCopyPullRequest
	actionLinkPullRequest
	actionDelete
	actionFilter
	actionEscape
	actionRelaunch
	actionUpgrade
	actionQuit
)

type binding struct {
	// keys are Bubble Tea keystrokes; a chord is two of them with a space
	// between.
	keys       []string
	action     action
	footerKey  string
	footerHelp string
}

var bindings = []binding{
	{keys: []string{"j", "down"}, action: actionDown, footerKey: "j/k", footerHelp: "move"},
	{keys: []string{"k", "up"}, action: actionUp},
	{keys: []string{"g g"}, action: actionTop},
	{keys: []string{"G"}, action: actionBottom},
	{keys: []string{"ctrl+d"}, action: actionHalfPageDown},
	{keys: []string{"ctrl+u"}, action: actionHalfPageUp},
	{keys: []string{"ctrl+f"}, action: actionPageDown},
	{keys: []string{"ctrl+b"}, action: actionPageUp},
	{keys: []string{"enter", "l"}, action: actionActivate, footerKey: "⏎", footerHelp: "show"},
	{keys: []string{"n"}, action: actionNew, footerKey: "n", footerHelp: "new"},
	{keys: []string{"t"}, action: actionTogglePin, footerKey: "t", footerHelp: "pin"},
	{keys: []string{"s"}, action: actionSnooze, footerKey: "s", footerHelp: "snooze"},
	{keys: []string{"u"}, action: actionWake, footerKey: "u", footerHelp: "wake"},
	{keys: []string{"a"}, action: actionAlias, footerKey: "a", footerHelp: "alias"},
	{keys: []string{"o"}, action: actionOpenPullRequest, footerKey: "o", footerHelp: "PR"},
	{keys: []string{"y"}, action: actionCopyPullRequest},
	{keys: []string{"P"}, action: actionLinkPullRequest},
	{keys: []string{"x"}, action: actionSettle, footerKey: "x", footerHelp: "settle"},
	{keys: []string{"ctrl+x"}, action: actionDelete},
	{keys: []string{"tab"}, action: actionNextSection, footerKey: "⇥", footerHelp: "section"},
	{keys: []string{"shift+tab"}, action: actionPrevSection},
	{keys: []string{"z a"}, action: actionFoldToggle, footerKey: "za", footerHelp: "fold"},
	{keys: []string{"z o"}, action: actionFoldOpen},
	{keys: []string{"z c", "h"}, action: actionFoldClose},
	{keys: []string{"/"}, action: actionFilter, footerKey: "/", footerHelp: "filter"},
	{keys: []string{"esc"}, action: actionEscape},
	{keys: []string{"R"}, action: actionRelaunch},
	{keys: []string{"U"}, action: actionUpgrade},
	{keys: []string{"q", "ctrl+c"}, action: actionQuit, footerKey: "q", footerHelp: "quit"},
}

const chordTimeout = time.Second

type keymap struct {
	pending   string
	pendingAt time.Time
}

func (k *keymap) press(key string, now time.Time) action {
	if k.pending != "" && now.Sub(k.pendingAt) <= chordTimeout {
		chord := k.pending + " " + key
		k.pending = ""
		return lookup(chord)
	}
	k.pending = ""
	if startsChord(key) {
		k.pending, k.pendingAt = key, now
		return actionNone
	}
	return lookup(key)
}

func lookup(key string) action {
	for _, b := range bindings {
		if slices.Contains(b.keys, key) {
			return b.action
		}
	}
	return actionNone
}

func startsChord(key string) bool {
	for _, b := range bindings {
		for _, k := range b.keys {
			if strings.HasPrefix(k, key+" ") {
				return true
			}
		}
	}
	return false
}
