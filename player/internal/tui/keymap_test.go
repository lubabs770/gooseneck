package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParseKey(t *testing.T) {
	for _, name := range []string{"j", "K", "ctrl+j", "alt+x", "enter", "esc", "tab", "shift+tab", "up", "pgdown", "backspace", "?", "א"} {
		k, ok := parseKey(name)
		if !ok || k.String() != name {
			t.Errorf("parseKey(%q) = %q, %v", name, k.String(), ok)
		}
	}
	if k, ok := parseKey("space"); !ok || k.String() != " " {
		t.Errorf("space = %q, %v", k.String(), ok)
	}
	for _, bad := range []string{"", "jk", "ctrl+banana", "alt+"} {
		if _, ok := parseKey(bad); ok {
			t.Errorf("parseKey(%q) should fail", bad)
		}
	}
}

func TestKeyMapSwapAndTyping(t *testing.T) {
	km, bad := newKeyMap(map[string]string{"j": "k", "k": "j", "ctrl+j": "ctrl+k", "nope": "k", "x": "ctrl+c?", "ctrl+c": "q"})
	if len(bad) != 3 {
		t.Fatalf("bad = %v, want 3 entries", bad)
	}
	press := func(s string) tea.KeyMsg { k, _ := parseKey(s); return k }
	cases := []struct {
		in     string
		typing bool
		want   string
	}{
		{"j", false, "k"}, {"k", false, "j"}, // swapped, no chaining
		{"h", false, "h"},          // untouched keys keep their default
		{"j", true, "j"},           // still types into search
		{"ctrl+j", true, "ctrl+k"}, // non-printable keys remap while typing
	}
	for _, c := range cases {
		if got := km.remap(press(c.in), c.typing).String(); got != c.want {
			t.Errorf("remap(%q, typing=%v) = %q, want %q", c.in, c.typing, got, c.want)
		}
	}
	if pasted := (tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j"), Paste: true}); km.remap(pasted, false).Paste != true {
		t.Error("pasted text must not remap")
	}
}
