package main

import (
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.3.0-dev", 1},
		{"0.3.0", "0.3.0-dev.12+abc", 1},
		{"0.3.0", "0.3.0-pre3", 1},
		{"0.3.0-dev.10+a", "0.3.0-dev.9+b", 1},
		{"0.3.1-dev.1", "0.3.0", 1},
		{"0.3.0", "0.2.9", 1},
		{"v0.3.0", "0.3.0", 0},
		{"0.3.0+abc", "0.3.0", 0},
		{"0.10.0", "0.9.0", 1},
		{"0.3.0-rc.1", "0.3.0-beta.2", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := compareVersions(c.b, c.a); got != -c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

func TestAppVersionFromFile(t *testing.T) {
	if appVersion != strings.TrimSpace(versionFile)+"-dev" {
		t.Errorf("appVersion = %q", appVersion)
	}
}
