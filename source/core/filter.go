package core

// Word filter: words you never want to see are masked (•••) in chat, history and
// notifications. Settings → Highlights → Word filter. Logs keep the original text.

import (
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

var filterCache struct {
	mu  sync.Mutex
	key string
	re  *regexp.Regexp
}

// filterRegexp builds one case-insensitive, whole-word pattern from the list; * is a wildcard.
func filterRegexp(words []string) *regexp.Regexp {
	key := strings.Join(words, "\x00")
	filterCache.mu.Lock()
	defer filterCache.mu.Unlock()
	if key == filterCache.key {
		return filterCache.re
	}
	var alts []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" || w == "*" {
			continue
		}
		parts := strings.Split(w, "*")
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		alts = append(alts, strings.Join(parts, `[\pL\pN_]*`))
	}
	var re *regexp.Regexp
	if len(alts) > 0 {
		// whole words: not touching another letter, digit or underscore on either side
		re, _ = regexp.Compile(`(?i)(^|[^\pL\pN_])(` + strings.Join(alts, "|") + `)([^\pL\pN_]|$)`)
	}
	filterCache.key, filterCache.re = key, re
	return re
}

func maskWords(re *regexp.Regexp, s string) string {
	if re == nil || s == "" {
		return s
	}
	// run twice: adjacent matches share the separator between them
	for i := 0; i < 2; i++ {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			sm := re.FindStringSubmatch(m)
			return sm[1] + strings.Repeat("•", utf8.RuneCountInString(sm[2])) + sm[3]
		})
	}
	return s
}

// maskFiltered masks the filtered words in a line of chat.
func (a *App) maskFiltered(s string) string {
	return maskWords(filterRegexp(a.store.Get().Settings.FilterWords), s)
}
