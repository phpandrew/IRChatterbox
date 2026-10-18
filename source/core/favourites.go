package core

// Favourites: channels and private chats pinned to a "Favourites" section at the top of the
// sidebar, across all servers, in the order you add (or drag) them.

import "strings"

type Favourite struct {
	Server string `json:"server"`
	Buffer string `json:"buffer"`
}

// SetFavourite adds a buffer to the favourites (at the end) or removes it.
func (a *App) SetFavourite(server, buffer string, on bool) {
	if server == "" || strings.TrimSpace(buffer) == "" || strings.HasPrefix(buffer, "*") {
		return
	}
	a.store.Update(func(c *Config) {
		keep := c.Favourites[:0]
		for _, f := range c.Favourites {
			if !(f.Server == server && strings.EqualFold(f.Buffer, buffer)) {
				keep = append(keep, f)
			}
		}
		c.Favourites = keep
		if on {
			c.Favourites = append(c.Favourites, Favourite{Server: server, Buffer: buffer})
		}
	})
	a.emitUI("config", a.store.Get())
}

// SetFavouriteOrder saves the favourites in a new order (after dragging). Entries not in the
// current list are ignored.
func (a *App) SetFavouriteOrder(order []Favourite) {
	a.store.Update(func(c *Config) {
		var out []Favourite
		used := map[int]bool{}
		for _, o := range order {
			for i, f := range c.Favourites {
				if !used[i] && f.Server == o.Server && strings.EqualFold(f.Buffer, o.Buffer) {
					out = append(out, f)
					used[i] = true
					break
				}
			}
		}
		for i, f := range c.Favourites {
			if !used[i] {
				out = append(out, f)
			}
		}
		c.Favourites = out
	})
}
