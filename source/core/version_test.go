package core

import (
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

func TestSetVersion(t *testing.T) {
	old := appVersion
	defer func() { appVersion = old }()
	SetVersion("", "0.4.0\n")
	if appVersion != "0.4.0-dev" {
		t.Errorf("from file: appVersion = %q", appVersion)
	}
	SetVersion("0.4.2", "0.4.0")
	if appVersion != "0.4.2" {
		t.Errorf("full: appVersion = %q", appVersion)
	}
}
