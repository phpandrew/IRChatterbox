package core

// Replies and reactions (IRCv3 client tags +draft/reply and +draft/react).
//
// A reply is an ordinary PRIVMSG carrying +draft/reply=<msgid of the message answered>;
// clients without support just see the message. A reaction is a TAGMSG carrying
// +draft/react=<emoji> and +draft/reply=<msgid>; clients without support see nothing.
// Removing a reaction sends +draft/unreact (understood by some clients, ignored by others).
// Both need the server's message-tags capability and a msgid on the message.

import (
	"strings"
	"time"
	"unicode/utf8"
)

var tagEscape = strings.NewReplacer(`\`, `\\`, ";", `\:`, " ", `\s`, "\r", `\r`, "\n", `\n`)

// replyTag returns the msgid a message answers or reacts to, if any.
func replyTag(m *Message) string {
	for _, k := range []string{"+draft/reply", "+reply"} {
		if v := m.Tags[k]; validMsgid(v) {
			return v
		}
	}
	return ""
}

func validMsgid(id string) bool {
	return id != "" && len(id) <= 128 && !strings.ContainsAny(id, " \r\n\x00;")
}

// validReaction accepts a short emoji or word: no spaces, at most 32 bytes.
func validReaction(r string) bool {
	return r != "" && len(r) <= 32 && utf8.ValidString(r) && !strings.ContainsAny(r, " \r\n\x00")
}

// Reply sends text as a reply to the message with id msgid.
func (c *Conn) Reply(target, msgid, text string) {
	if c.Status() != "connected" {
		c.info(target, "error", "Not connected")
		return
	}
	tags := ""
	extra := map[string]any{}
	if validMsgid(msgid) && c.clientTagAllowed("reply") {
		tags = "+draft/reply=" + tagEscape.Replace(msgid)
		extra["reply"] = msgid
	}
	for _, chunk := range splitText(text, c.maxPayload("PRIVMSG", target)-len(tags)) {
		if !c.sendMsgTags("PRIVMSG", target, chunk, tags, false) {
			c.emitOwnTagged(target, target, "privmsg", chunk, extra)
			c.app.plugins.OnMessage(c, target, c.Nick(), chunk, true)
		}
	}
}

// SendReply is called by the UI to answer a message. Commands ("/me …") are run as usual.
func (a *App) SendReply(server, buffer, msgid, text string) {
	c := a.conn(server)
	if c == nil {
		return
	}
	text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if strings.TrimSpace(text) == "" {
		return
	}
	if strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "//") || strings.Contains(text, "\n") {
		a.SendInput(server, buffer, text)
		return
	}
	text = strings.TrimPrefix(text, "/")
	c.Reply(buffer, msgid, a.expandShortcuts(text))
}

// React adds (or with remove, takes back) an emoji reaction to a message. Returns an error
// message for the UI, or "".
func (a *App) React(server, buffer, msgid, emoji string, remove bool) string {
	c := a.conn(server)
	if c == nil || c.Status() != "connected" {
		return "Not connected"
	}
	if !validMsgid(msgid) || !validReaction(emoji) {
		return "Can't react to that message"
	}
	if !c.clientTagAllowed("react") {
		return "This server doesn't support reactions"
	}
	tag := "+draft/react="
	if remove {
		tag = "+draft/unreact="
	}
	c.SendRaw("@" + tag + tagEscape.Replace(emoji) + ";+draft/reply=" + tagEscape.Replace(msgid) + " TAGMSG " + buffer)
	if !c.hasCap("echo-message") {
		c.emit(Event{Type: "react", Buffer: buffer, Nick: c.Nick(), Text: emoji, Time: time.Now().UnixMilli(), Self: true,
			Extra: map[string]any{"msgid": msgid, "remove": remove}})
	}
	return ""
}

// onReact handles a reaction TAGMSG. It reports whether the TAGMSG was one.
func (c *Conn) onReact(m *Message, nick string, isMe bool) bool {
	emoji, remove := m.Tags["+draft/react"], false
	if emoji == "" {
		emoji, remove = m.Tags["+draft/unreact"], true
	}
	if emoji == "" {
		emoji, remove = m.Tags["+react"], false
	}
	if emoji == "" {
		return false
	}
	msgid := replyTag(m)
	if !validReaction(emoji) || msgid == "" || c.app.isIgnored(nick, m.Prefix) {
		return true
	}
	target := m.Param(0)
	buffer := target
	if !c.isChannel(target) && !isMe {
		buffer = nick
	}
	c.emit(Event{Type: "react", Buffer: buffer, Nick: nick, Text: emoji, Time: m.Time().UnixMilli(), Self: isMe,
		Extra: map[string]any{"msgid": msgid, "remove": remove}})
	return true
}
