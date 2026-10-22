package core

import "strings"

// toastText makes text safe for a Windows notification. go-toast puts the title and body
// inside <![CDATA[…]]> in its XML, unescaped, and its PowerShell fallback puts that XML in a
// PowerShell "@"…"@ string, where $(…) runs code. So "]]>" in a message could add XML to the
// notification, and "$(…)" could run a command when the fallback is used.
//
// The result closes the CDATA section, gives the text as escaped XML (with $ and ` as
// character references, which PowerShell leaves alone and the XML parser turns back into the
// same characters) and opens an empty CDATA section again for the template to close.
func toastText(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("]]>")
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '$':
			b.WriteString("&#36;")
		case r == '`':
			b.WriteString("&#96;")
		case r == '"':
			b.WriteString("&#34;")
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteByte(' ')
		case r < 0x20 || r == 0xFFFE || r == 0xFFFF || (r >= 0xD800 && r <= 0xDFFF):
			// not allowed in XML (IRC colour and format codes): drop
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("<![CDATA[")
	return b.String()
}
