package termsafe

import "testing"

func TestText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{"two\nlines\tand tab", "two\nlines\tand tab"},
		{"\x1b[31mred\x1b[0m", `\x1b[31mred\x1b[0m`},
		{"\x1b]0;pwned\x07", `\x1b]0;pwned\x07`},
		{"carriage\rreturn", `carriage\x0dreturn`},
		{"c1 \u009b csi", `c1 \x9b csi`},
		{"del\x7f", `del\x7f`},
		{"bidi " + string(rune(0x202E)) + "evil", "bidi " + string('\\') + "u202eevil"},
		{"emoji ok 🚀 ไทย", "emoji ok 🚀 ไทย"},
	}
	for _, tt := range tests {
		if got := Text(tt.in); got != tt.want {
			t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStyled(t *testing.T) {
	esc := "\x1b"
	tests := []struct{ in, want string }{
		{esc + "[1mbold" + esc + "[0m", esc + "[1mbold" + esc + "[0m"},
		{esc + "[38;5;252mcolor" + esc + "[0m", esc + "[38;5;252mcolor" + esc + "[0m"},
		{"x" + esc + "]0;pwned\x07y", `x\x1b]0;pwned\x07y`},
		{esc + "[2J", `\x1b[2J`},
		{esc + "]52;c;ZXZpbA==\x07", `\x1b]52;c;ZXZpbA==\x07`},
		{"lines\nstay", "lines\nstay"},
		{"trailing " + esc, `trailing \x1b`},
	}
	for _, tt := range tests {
		if got := Styled(tt.in); got != tt.want {
			t.Errorf("Styled(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLine(t *testing.T) {
	if got := Line("a\nb\tc\x1b"); got != `a b c\x1b` {
		t.Fatalf("got %q", got)
	}
}
