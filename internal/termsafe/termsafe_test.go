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

func TestLine(t *testing.T) {
	if got := Line("a\nb\tc\x1b"); got != `a b c\x1b` {
		t.Fatalf("got %q", got)
	}
}
