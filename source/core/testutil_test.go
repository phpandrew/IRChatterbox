package core

import (
	"sync"
	"testing"
	"time"
)

// evLog collects the events an App emits, for tests.
type evLog struct {
	mu  sync.Mutex
	evs []Event
	all []Event // never trimmed by waitFor
}

func (l *evLog) add(e Event) { l.mu.Lock(); l.evs = append(l.evs, e); l.all = append(l.all, e); l.mu.Unlock() }
func (l *evLog) waitFor(t *testing.T, desc string, f func(Event) bool) Event {
	end := time.Now().Add(6 * time.Second)
	for time.Now().Before(end) {
		l.mu.Lock()
		for i, e := range l.evs {
			if f(e) {
				l.evs = l.evs[i+1:]
				l.mu.Unlock()
				return e
			}
		}
		l.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for event: %s", desc)
	return Event{}
}
