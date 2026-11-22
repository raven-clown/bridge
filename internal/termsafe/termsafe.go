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

// Styled is Text for output of a renderer such as glamour: it keeps SGR
// sequences (ESC [ digits and semicolons m), which only set colors and
// weight, and escapes every other control character. A Markdown renderer
// decodes entities like &#27; after Text has run, so its output needs
// this second pass.
func Styled(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == 0x1b {
			if n := sgrLen(rs[i:]); n > 0 {
				b.WriteString(string(rs[i : i+n]))
				i += n - 1
				continue
			}
		}
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

// sgrLen returns the length of an SGR sequence at the start of rs, or 0.
func sgrLen(rs []rune) int {
	if len(rs) < 3 || rs[0] != 0x1b || rs[1] != '[' {
		return 0
	}
	for i := 2; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == 'm':
			return i + 1
		case (r >= '0' && r <= '9') || r == ';':
		default:
			return 0
		}
	}
	return 0
}

// Line is Text for one-line fields such as titles, logins and branch
// names: newlines and tabs become spaces.
func Line(s string) string {
	return Text(strings.NewReplacer("\r\n", " ", "\n", " ", "\t", " ").Replace(s))
}
