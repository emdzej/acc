package server

import "testing"

func TestIsSlash(t *testing.T) {
	for body, want := range map[string]bool{
		"/compact":           true,
		"  /rename foo bar ": true,
		"/":                  false,
		"// comment":         false,
		"/ not a command":    false,
		"/rename a\nmore":    false,
		"please /compact":    false,
		"/tmp/x is broken":   true, // Claude itself treats a leading / as a command too
	} {
		if got := isSlash(body); got != want {
			t.Errorf("isSlash(%q) = %v, want %v", body, got, want)
		}
	}
}
