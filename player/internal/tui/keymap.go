package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// keyMap is the [keys] table from config.toml: pressed key → the default key
// it acts as. The TUI's handlers only know the default keys; key() translates
// every press through this once, so overrides reach every page and dialog.
// Lookups don't chain, which is what makes a plain swap (j=k, k=j) work.
type keyMap map[string]tea.KeyMsg

// newKeyMap validates the config table. Bad entries are skipped and reported
// so one typo doesn't take the whole table down.
func newKeyMap(raw map[string]string) (keyMap, []string) {
	km := keyMap{}
	var bad []string
	for from, to := range raw {
		f, okF := parseKey(from)
		t, okT := parseKey(to)
		switch {
		case !okF:
			bad = append(bad, fmt.Sprintf("unknown key %q", from))
		case !okT:
			bad = append(bad, fmt.Sprintf("unknown key %q (for %q)", to, from))
		case f.String() == "ctrl+c":
			bad = append(bad, "ctrl+c always quits")
		default:
			km[f.String()] = t
		}
	}
	sort.Strings(bad)
	return km, bad
}

// remap returns the default key that msg stands for. While typing (search,
// command palette) printable keys are left alone so they still type.
func (km keyMap) remap(msg tea.KeyMsg, typing bool) tea.KeyMsg {
	if typing && printable(msg) {
		return msg
	}
	if to, ok := km[msg.String()]; ok {
		return to
	}
	return msg
}

// pairs lists the overrides for the help dialog: {"j", "acts as k"}.
func (km keyMap) pairs() [][2]string {
	out := make([][2]string, 0, len(km))
	for from, to := range km {
		out = append(out, [2]string{keyLabel(from), "acts as " + keyLabel(to.String())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func printable(msg tea.KeyMsg) bool {
	return !msg.Alt && (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace)
}

func keyLabel(s string) string {
	if s == " " {
		return "space"
	}
	return s
}

// namedKeys maps bubbletea's key names ("enter", "ctrl+j", "up", …) back to
// their types. bubbletea doesn't export this table, so build it by asking
// every key type for its name.
var namedKeys = func() map[string]tea.KeyType {
	m := map[string]tea.KeyType{}
	for t := tea.KeyType(-300); t <= 300; t++ {
		if t == tea.KeyRunes {
			continue
		}
		if name := t.String(); name != "" {
			m[name] = t
		}
	}
	return m
}()

// parseKey turns a config key name into the KeyMsg bubbletea would send for
// it: "k", "K", "ctrl+j", "alt+x", "enter", "space", "pgdown", …
func parseKey(s string) (tea.KeyMsg, bool) {
	alt := false
	if rest, ok := strings.CutPrefix(s, "alt+"); ok && rest != "" {
		alt, s = true, rest
	}
	if s == "space" {
		s = " "
	}
	if t, ok := namedKeys[s]; ok {
		return tea.KeyMsg{Type: t, Alt: alt}, true
	}
	if utf8.RuneCountInString(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Alt: alt}, true
	}
	return tea.KeyMsg{}, false
}
