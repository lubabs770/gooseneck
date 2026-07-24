package main

import "testing"

func TestVis(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Aaron Cohen", "Aaron Cohen"},          // pure LTR untouched
		{"שלום", "םולש"},                          // pure Hebrew reversed to visual
		{"(ווקאלי)", "(ילאקוו)"},                 // brackets mirrored
	}
	for _, c := range cases {
		if got := vis(c.in); got != c.want {
			t.Errorf("vis(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// mixed LTR+RTL should at least run and change order of the RTL part
	mixed := vis("Aaron Razel - אהרן רזאל")
	t.Logf("mixed -> %q", mixed)
}
