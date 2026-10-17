package mobile

import (
	"errors"
	"strings"
	"testing"
)

type fake struct{ got []any }

type item struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func (f *fake) Nothing()                    {}
func (f *fake) Echo(s string, n int) string { return strings.Repeat(s, n) }
func (f *fake) Save(it item) item           { it.N++; return it }
func (f *fake) Fails() error                { return errors.New("boom") }
func (f *fake) Pair(ok bool) (string, error) {
	if ok {
		return "yes", nil
	}
	return "", errors.New("no")
}
func (f *fake) Panics()               { panic("oops") }
func (f *fake) List(ids []string) int { return len(ids) }

func TestDispatch(t *testing.T) {
	f := &fake{}
	cases := []struct{ method, args, want string }{
		{"Nothing", "[]", `{"ok":null}`},
		{"Echo", `["ab",3]`, `{"ok":"ababab"}`},
		{"Echo", `["ab"]`, `{"ok":""}`}, // missing args are zero values: "" repeated 0 times
		{"Save", `[{"name":"x","n":1}]`, `{"ok":{"name":"x","n":2}}`},
		{"Fails", `[]`, `{"error":"boom"}`},
		{"Pair", `[true]`, `{"ok":"yes"}`},
		{"Pair", `[false]`, `{"error":"no"}`},
		{"Panics", `[]`, `{"error":"Panics failed: oops"}`},
		{"List", `[["a","b"]]`, `{"ok":2}`},
		{"Missing", `[]`, `{"ok":null}`},
		{"Echo", `[1,2]`, `{"error":"bad argument 1 for Echo: json: cannot unmarshal number into Go value of type string"}`},
	}
	for _, c := range cases {
		if got := encodeReply(dispatch(f, c.method, c.args)); got != c.want {
			t.Errorf("%s(%s) = %s, want %s", c.method, c.args, got, c.want)
		}
	}
}
