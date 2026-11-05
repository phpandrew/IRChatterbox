package core

import "testing"

func TestDCCNamesStayInTheFolder(t *testing.T) {
	for in, want := range map[string]string{`..\..\evil.exe`: "evil.exe", "../../x": "x", "..": "file", ".": "file", "CON": "_CON", "a/b\\c": "c", "x\x00y": "x_y", " .hidden. ": "hidden", "evil\u202egnp.exe": "evil_gnp.exe", "a\u200bb": "a_b"} {
		if got := cleanFileName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
