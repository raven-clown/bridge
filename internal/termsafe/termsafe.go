// Package termsafe makes text from the forge safe to print to a terminal.
// Titles, bodies and comments come from anyone who can open an issue, so
// escape sequences in them must never reach the terminal.
package termsafe

import (
	"fmt"
	"strings"
	"unicode"
)

func bidi(r rune) bool {
	return (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0x200E || r == 0x200F
}

func escape(b *strings.Builder, r rune) {
	if r < 0x100 {
		fmt.Fprintf(b, `\x%02x`, r)
		return
	}
	fmt.Fprintf(b, `\u%04x`, r)
}

// Text escapes control characters and bidi overrides, keeping newlines
// and tabs. Use it for bodies and comments.
func Text(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r) || bidi(r):
			escape(&b, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Line is Text for one-line fields such as titles, logins and branch
// names: newlines and tabs become spaces.
func Line(s string) string {
	return Text(strings.NewReplacer("\r\n", " ", "\n", " ", "\t", " ").Replace(s))
}
