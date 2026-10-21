package main

import "testing"

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v1.0.1", "1.0.0", true}, {"1.1", "1.0.9", true}, {"v2.0.0-beta", "1.9.9", true},
		{"1.0.0", "1.0.0", false}, {"v0.9.0", "1.0.0", false}, {"1.0", "1.0.0", false},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
