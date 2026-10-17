package main

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"
)

// Older networks and clients often send Latin-1/Windows-1252 (Western), Windows-1251/KOI8-R
// (Cyrillic) and so on instead of UTF-8. Each server has an Encoding setting:
//   ""/"auto" — UTF-8, but any line that isn't valid UTF-8 is read as Windows-1252
//   "utf-8"   — strict UTF-8 (invalid bytes shown as �)
//   anything else (windows-1251, iso-8859-2, shift_jis, …) — read and sent in that charset

// Encodings offered in the server editor (value, label).
var EncodingChoices = [][2]string{
	{"auto", "Automatic (UTF-8, fall back to Western)"},
	{"utf-8", "UTF-8 only"},
	{"windows-1252", "Western (Windows-1252)"},
	{"iso-8859-1", "Western (ISO-8859-1)"},
	{"iso-8859-15", "Western (ISO-8859-15)"},
	{"windows-1250", "Central European (Windows-1250)"},
	{"iso-8859-2", "Central European (ISO-8859-2)"},
	{"windows-1251", "Cyrillic (Windows-1251)"},
	{"koi8-r", "Cyrillic (KOI8-R)"},
	{"windows-1253", "Greek (Windows-1253)"},
	{"windows-1254", "Turkish (Windows-1254)"},
	{"windows-1255", "Hebrew (Windows-1255)"},
	{"windows-1256", "Arabic (Windows-1256)"},
	{"windows-1257", "Baltic (Windows-1257)"},
	{"windows-874", "Thai (Windows-874)"},
	{"shift_jis", "Japanese (Shift JIS)"},
	{"euc-jp", "Japanese (EUC-JP)"},
	{"iso-2022-jp", "Japanese (ISO-2022-JP)"},
	{"gbk", "Chinese Simplified (GBK)"},
	{"big5", "Chinese Traditional (Big5)"},
	{"euc-kr", "Korean (EUC-KR)"},
}

func lookupEncoding(name string) encoding.Encoding {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto", "utf-8", "utf8":
		return nil
	}
	e, err := htmlindex.Get(name)
	if err != nil {
		return nil
	}
	return e
}

func (c *Conn) decode(raw string) string {
	mode := strings.ToLower(c.cfg.Encoding)
	switch mode {
	case "", "auto":
		if utf8.ValidString(raw) {
			return raw
		}
		if out, err := charmap.Windows1252.NewDecoder().String(raw); err == nil {
			return out
		}
		return strings.ToValidUTF8(raw, "\uFFFD")
	case "utf-8", "utf8":
		return strings.ToValidUTF8(raw, "\uFFFD")
	}
	if e := lookupEncoding(mode); e != nil {
		if out, err := e.NewDecoder().String(raw); err == nil {
			return out
		}
	}
	return strings.ToValidUTF8(raw, "\uFFFD")
}

func (c *Conn) encode(line string) string {
	if e := lookupEncoding(c.cfg.Encoding); e != nil {
		if out, err := encoding.ReplaceUnsupported(e.NewEncoder()).String(line); err == nil {
			return out
		}
	}
	return line
}

// GetEncodings lists the encodings for the server editor.
func (a *App) GetEncodings() [][2]string { return EncodingChoices }
