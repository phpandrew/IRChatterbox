package core

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestToastTextIsInertXML(t *testing.T) {
	for _, in := range []string{
		"hi",
		`]]></text></binding></visual><actions><action activationType="protocol" content="x" arguments="file:///C:/Windows/System32/calc.exe"/></actions><visual><binding template="ToastGeneric"><text><![CDATA[`,
		`$(Start-Process calc) and ${env:USERNAME} and ` + "`0`n" + ` "@ `,
		"\x02bold\x02 \x0304red\x03 \x00nul <b>&amp;",
		"<image src='\\\\evil\\share\\x.png'/>",
	} {
		frag := "<text><![CDATA[" + toastText(in) + "]]></text>"
		var got struct {
			XMLName xml.Name   `xml:"text"`
			Inner   string     `xml:",chardata"`
			Any     []xml.Name `xml:",any"`
		}
		if err := xml.Unmarshal([]byte(frag), &got); err != nil {
			t.Fatalf("%q: invalid XML %v\n%s", in, err, frag)
		}
		if len(got.Any) != 0 {
			t.Fatalf("%q: added elements %v", in, got.Any)
		}
		if strings.Contains(toastText(in), "$") || strings.Contains(toastText(in), "`") {
			t.Fatalf("%q: PowerShell-active characters left in %q", in, toastText(in))
		}
		if strings.ContainsAny(in, "$<") && !strings.ContainsAny(got.Inner, "$<") {
			t.Fatalf("%q: text changed: %q", in, got.Inner)
		}
	}
}
