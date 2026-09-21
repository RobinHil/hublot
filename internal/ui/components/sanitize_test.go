package components

import (
	"strings"
	"testing"
)

// Everything here is something a container can print on its standard output,
// which reaches this screen unchanged unless Sanitize stops it.
func TestSanitizeRefusesTerminalControl(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string
		about string
	}{
		{
			name:  "cursor movement",
			in:    "safe\x1b[2Aspoofed",
			want:  "safespoofed",
			about: "moving the cursor lets output overwrite the interface above it",
		},
		{
			name:  "screen clearing",
			in:    "before\x1b[2Jafter",
			want:  "beforeafter",
			about: "clearing the display would blank the table around the logs",
		},
		{
			name:  "window title through OSC",
			in:    "log line\x1b]0;owned\x07 continues",
			want:  "log line continues",
			about: "OSC 0 sets the terminal title from inside a container",
		},
		{
			name:  "clipboard through OSC 52",
			in:    "x\x1b]52;c;b3duZWQ=\x1b\\y",
			want:  "xy",
			about: "OSC 52 writes the user's clipboard",
		},
		{
			name:  "device control string",
			in:    "a\x1bPq#0;2;0;0;0\x1b\\b",
			want:  "ab",
			about: "DCS carries whole sub-protocols, sixel among them",
		},
		{
			name:  "application program command",
			in:    "a\x1b_payload\x1b\\b",
			want:  "ab",
			about: "APC is passed to the terminal to interpret",
		},
		{
			name:  "eight-bit CSI",
			in:    "a\xc2\x9b31mb",
			want:  "a31mb",
			about: "the C1 introducer is CSI in one byte and is often forgotten",
		},
		{
			name:  "carriage return and backspace",
			in:    "real\r\x08fake",
			want:  "realfake",
			about: "a carriage return rewrites the line that was just printed",
		},
		{
			name:  "bell",
			in:    "quiet\x07",
			want:  "quiet",
			about: "a container should not be able to ring the terminal",
		},
		{
			name:  "lone escape",
			in:    "a\x1b",
			want:  "a",
			about: "a truncated sequence must not leave an escape in the output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sanitize(tt.in)
			if got != tt.want {
				t.Errorf("%s\ngot  %q\nwant %q", tt.about, got, tt.want)
			}
			if strings.ContainsRune(got, 0x1b) {
				t.Errorf("an escape survived: %q", got)
			}
		})
	}
}

func TestSanitizeKeepsColour(t *testing.T) {
	// An application's own colours are worth keeping: they cannot move the
	// cursor or address the terminal, and log output leans on them.
	in := "\x1b[31merror\x1b[0m: could not connect"
	got := Sanitize(in)
	if got != in {
		t.Errorf("colour should survive:\ngot  %q\nwant %q", got, in)
	}

	// A CSI that ends in m but carries private parameters is not a colour.
	if got := Sanitize("a\x1b[?25lb"); got != "ab" {
		t.Errorf("a private sequence must go: %q", got)
	}
	if got := Sanitize("a\x1b[>1mb"); got != "ab" {
		t.Errorf("a private SGR-looking sequence must go: %q", got)
	}
}

func TestSanitizeKeepsOrdinaryText(t *testing.T) {
	cases := []string{
		"2026/09/21 08:26:03 [notice] 1#1: start worker process",
		"accents: éàü, emoji fall through as text",
		"",
	}
	for _, in := range cases {
		if got := Sanitize(in); got != in {
			t.Errorf("Sanitize(%q) = %q", in, got)
		}
	}

	// Tabs become spaces so a column cannot be pushed out of alignment.
	if got := Sanitize("a\tb"); got != "a    b" {
		t.Errorf("tab handling: %q", got)
	}
}

func TestSanitizeWidth(t *testing.T) {
	if got := SanitizeWidth("abcdefghij", 4); got != "abcd" {
		t.Errorf("truncation: %q", got)
	}
	if got := SanitizeWidth("a\x1b[2Jb", 10); got != "ab" {
		t.Errorf("control characters go before the width is measured: %q", got)
	}
	if got := SanitizeWidth("abc", 0); got != "" {
		t.Errorf("zero width: %q", got)
	}
}
