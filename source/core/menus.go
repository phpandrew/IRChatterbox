package core

// Right-click menu items added by scripts: mIRC "menu nicklist { … }" blocks and the JS
// menu(where, label, fn) call. The UI asks for the items when a menu opens (PluginMenu)
// and runs the one you click (RunPluginMenu).

import (
	"strconv"
	"strings"
)

// Menu kinds: nicklist (a nick, in the user list or chat), channel, query, status (server),
// message (a chat line; JavaScript only).
var menuKinds = map[string]bool{"nicklist": true, "channel": true, "query": true, "status": true, "message": true}

// MenuItem is one entry sent to the UI. A separator has Sep; a submenu has Items.
type MenuItem struct {
	ID    string     `json:"id,omitempty"`
	Label string     `json:"label,omitempty"`
	Sep   bool       `json:"sep,omitempty"`
	Items []MenuItem `json:"items,omitempty"`
}

// MenuContext is what was right-clicked.
type MenuContext struct {
	Server string `json:"server"`
	Buffer string `json:"buffer"`
	Kind   string `json:"kind"`
	Nick   string `json:"nick"`
	Text   string `json:"text"` // the line's text, for "message"
}

// ---------- mIRC ----------

type mircMenuNode struct {
	label    string // unevaluated, may contain identifiers
	body     []mStmt
	sep      bool
	children []*mircMenuNode
	id       int
	file     string
}

type mircMenu struct {
	file  string
	kinds []string
	items []*mircMenuNode
}

// parseMenu: menu nicklist,query { Label:command | .Sub:command | - | Label:{ … } }
func (e *mircEngine) parseMenu(file, def string) error {
	lines := strings.Split(def, "\n")
	head := strings.TrimSpace(strings.TrimPrefix(lines[0], "menu"))
	head = strings.TrimSpace(strings.TrimSuffix(head, "{"))
	m := &mircMenu{file: file}
	for _, k := range strings.Split(head, ",") {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "@" || strings.HasPrefix(k, "@") || k == "menubar" {
			continue // custom windows and the menu bar don't exist here
		}
		if k == "*" {
			m.kinds = append(m.kinds, "nicklist", "channel", "query", "status")
		} else if menuKinds[k] {
			m.kinds = append(m.kinds, k)
		}
	}
	if len(m.kinds) == 0 {
		return nil
	}
	body := lines[1:]
	if n := len(body); n > 0 && strings.TrimSpace(body[n-1]) == "}" {
		body = body[:n-1]
	}
	// stack[d] is the list items at depth d are appended to
	stack := []*[]*mircMenuNode{&m.items}
	for i := 0; i < len(body); i++ {
		l := strings.TrimSpace(body[i])
		if l == "" {
			continue
		}
		depth := 0
		for depth < len(l) && l[depth] == '.' {
			depth++
		}
		l = l[depth:]
		node := &mircMenuNode{file: file}
		if l == "-" {
			node.sep = true
		} else if label, cmd, ok := cutMenuLabel(l); ok {
			node.label = label
			cmd = strings.TrimSpace(cmd)
			if strings.HasSuffix(cmd, "{") || cmd == "{" { // multi-line command block
				block := []string{cmd}
				d := braceDelta(cmd)
				for d > 0 && i+1 < len(body) {
					i++
					block = append(block, body[i])
					d += braceDelta(body[i])
				}
				cmd = strings.Join(block, "\n")
			}
			node.body = bodyOf(cmd)
		} else {
			node.label = l // a submenu heading; its items follow with one more dot
		}
		if depth >= len(stack) {
			depth = len(stack) - 1 // a jump of more than one level: attach to the deepest
		}
		stack = stack[:depth+1]
		parent := stack[depth]
		*parent = append(*parent, node)
		if !node.sep && node.body == nil {
			stack = append(stack, &node.children)
		}
		node.id = len(e.menuNodes)
		e.menuNodes = append(e.menuNodes, node)
	}
	e.menus = append(e.menus, m)
	return nil
}

// cutMenuLabel splits "Label:command" at the first ':' that isn't inside an identifier's
// parentheses (labels like $iif($me isop $chan, Op) contain ':' rarely, commas often).
func cutMenuLabel(l string) (string, string, bool) {
	depth := 0
	for i, r := range l {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ':':
			if depth == 0 {
				return strings.TrimSpace(l[:i]), l[i+1:], true
			}
		}
	}
	return l, "", false
}

func (x *mCtx) menuLabel(raw string) string {
	l := strings.TrimSpace(x.eval(raw))
	l = strings.ReplaceAll(l, "&&", "\x00")
	l = strings.ReplaceAll(l, "&", "")
	return strings.ReplaceAll(l, "\x00", "&")
}

func (e *mircEngine) menuCtx(c *Conn, mc MenuContext) *mCtx {
	x := e.newCtx(c)
	x.target = mc.Buffer
	if c != nil && c.isChannel(mc.Buffer) {
		x.chan_ = mc.Buffer
	}
	switch mc.Kind {
	case "nicklist":
		x.params = []string{mc.Nick}
		x.nick = mc.Nick
		x.extra["snicks"] = mc.Nick
	case "query":
		x.params = []string{mc.Buffer}
		x.nick = mc.Buffer
	case "channel":
		x.params = []string{mc.Buffer}
	}
	if x.nick == "" && c != nil {
		x.nick = c.Nick()
	}
	return x
}

func (e *mircEngine) menuItems(c *Conn, mc MenuContext) []MenuItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []MenuItem
	x := e.menuCtx(c, mc)
	var build func(nodes []*mircMenuNode) []MenuItem
	build = func(nodes []*mircMenuNode) []MenuItem {
		var items []MenuItem
		for _, n := range nodes {
			if n.sep {
				items = append(items, MenuItem{Sep: true})
				continue
			}
			label := x.menuLabel(n.label)
			if label == "" || label == "-" {
				continue // mIRC hides items whose label evaluates to nothing
			}
			if n.body == nil {
				if sub := build(n.children); len(sub) > 0 {
					items = append(items, MenuItem{Label: label, Items: sub})
				}
				continue
			}
			items = append(items, MenuItem{ID: "mirc:" + strconv.Itoa(n.id), Label: label})
		}
		return items
	}
	for _, m := range e.menus {
		for _, k := range m.kinds {
			if k == mc.Kind {
				out = append(out, build(m.items)...)
				break
			}
		}
	}
	return out
}

func (e *mircEngine) runMenu(c *Conn, mc MenuContext, id int) {
	e.mu.Lock()
	if id < 0 || id >= len(e.menuNodes) || e.menuNodes[id].body == nil {
		e.mu.Unlock()
		return
	}
	node := e.menuNodes[id]
	e.mu.Unlock()
	select {
	case e.jobs <- func() {
		x := e.menuCtx(c, mc)
		if err := x.run(node.body); err != nil && err != errHalt && err != errReturn {
			e.p.app.logScriptError(c, node.file, err)
		}
	}:
	default:
	}
}

// ---------- app ----------

// PluginMenu returns the script-added items for a right-click menu.
func (a *App) PluginMenu(mc MenuContext) []MenuItem {
	if !a.store.Get().Plugins.ScriptsEnabled || !menuKinds[mc.Kind] {
		return []MenuItem{}
	}
	c := a.conn(mc.Server)
	items := a.plugins.mirc.menuItems(c, mc)
	items = append(items, a.plugins.jsMenuItems(mc.Kind)...)
	if items == nil {
		return []MenuItem{}
	}
	return tidySeps(items)
}

// RunPluginMenu runs the clicked item.
func (a *App) RunPluginMenu(mc MenuContext, id string) {
	if !a.store.Get().Plugins.ScriptsEnabled {
		return
	}
	c := a.conn(mc.Server)
	if c == nil {
		return
	}
	switch {
	case strings.HasPrefix(id, "mirc:"):
		n, err := strconv.Atoi(strings.TrimPrefix(id, "mirc:"))
		if err == nil {
			a.plugins.mirc.runMenu(c, mc, n)
		}
	case strings.HasPrefix(id, "js:"):
		a.plugins.runJSMenu(c, mc, strings.TrimPrefix(id, "js:"))
	}
}

// tidySeps drops leading, trailing and doubled separators.
func tidySeps(items []MenuItem) []MenuItem {
	out := []MenuItem{}
	for _, it := range items {
		if it.Sep && (len(out) == 0 || out[len(out)-1].Sep) {
			continue
		}
		if len(it.Items) > 0 {
			it.Items = tidySeps(it.Items)
		}
		out = append(out, it)
	}
	for len(out) > 0 && out[len(out)-1].Sep {
		out = out[:len(out)-1]
	}
	return out
}
