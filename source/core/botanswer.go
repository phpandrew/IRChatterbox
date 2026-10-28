package core

import (
	"math/rand/v2"
	"runtime"
	"strings"
	"time"
)

// Several of your devices can share one bouncer login. A message from someone else reaches
// every one of them, and each would answer it, so a channel gets the same bot reply from the
// computer and the phone. A bot answer to someone else's message therefore waits a moment
// (a phone longer than a computer, plus a random part so two computers don't tie), and is
// dropped if another of your devices answered first. Answers to your own messages only run
// on the device you typed them on (see handlePrivmsg), so they go straight out.

type otherSaid struct {
	target, text string // lowercase target; text without formatting
	at           time.Time
}

// botWait is how long this device holds a bot answer to someone else's message.
var botWait = func() time.Duration {
	if runtime.GOOS == "android" {
		return 2*time.Second + rand.N(time.Second)
	}
	return 300*time.Millisecond + rand.N(700*time.Millisecond)
}

// noteOtherDevice records a line our nick sent from another device (seen through the bouncer).
func (c *Conn) noteOtherDevice(target, text string) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	keep := c.otherSaid[:0]
	for _, o := range c.otherSaid {
		if now.Sub(o.at) < time.Minute {
			keep = append(keep, o)
		}
	}
	if len(keep) >= 200 {
		keep = keep[1:]
	}
	c.otherSaid = append(keep, otherSaid{lower(target), strings.TrimSpace(stripFormatting(text)), now})
}

// answeredElsewhere reports whether another of our devices sent a bot line to target since
// `since`: one starting with the bot prefix, or (no prefix set) one of lines, or, with no
// prefix and the lines not known yet, anything at all.
func (c *Conn) answeredElsewhere(target string, since time.Time, prefix string, lines []string) bool {
	prefix = strings.TrimSpace(prefix)
	target = lower(target)
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.otherSaid {
		if o.target != target || o.at.Before(since) {
			continue
		}
		if prefix != "" {
			if strings.HasPrefix(o.text, prefix) {
				return true
			}
			continue
		}
		if lines == nil {
			return true
		}
		for _, l := range lines {
			if strings.TrimSpace(stripFormatting(l)) == o.text {
				return true
			}
		}
	}
	return false
}

// answer sends a bot's reply to replyTo. build builds the lines (it may fetch something) and
// returns none to say nothing. self: the trigger was typed on this device.
func (p *Plugins) answer(c *Conn, replyTo string, self bool, build func() []string) {
	prefix := p.app.store.Get().Plugins.BotPrefix
	shared := !self && c.cfg.Bouncer != ""
	start := time.Now()
	if shared {
		time.Sleep(botWait())
		if strings.TrimSpace(prefix) != "" && c.answeredElsewhere(replyTo, start, prefix, nil) {
			return // another device answered while we waited: don't fetch anything
		}
	} else {
		time.Sleep(150 * time.Millisecond) // keep it after the triggering line
	}
	lines := build()
	if len(lines) == 0 {
		return
	}
	if shared && c.answeredElsewhere(replyTo, start, prefix, lines) {
		return
	}
	for _, l := range lines {
		c.Say(replyTo, l, true)
	}
}
