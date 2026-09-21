package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Sanitize makes text from the daemon safe to draw.
//
// Container output is untrusted: whatever a process inside writes reaches this
// screen. Left alone, it can move the cursor, clear the display, redraw parts
// of the interface to say something that is not true, set the window title, or
// drive the terminal's clipboard and query responses through OSC. Log lines,
// inspect output, image names, labels and error messages all come from the
// same place and all go through here.
//
// Colour survives: SGR sequences are the one escape kept, because an
// application's own colours are useful and they cannot move the cursor or
// address the terminal. Everything else is dropped, along with the C0 and C1
// control characters, except tab, which is expanded so alignment holds.
func Sanitize(s string) string {
	if s == "" {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		c := s[i]

		if c == 0x1b {
			if seq, width, ok := readEscape(s[i:]); ok {
				// Only SGR, and only after re-emitting it from the parsed
				// parameters rather than passing the original bytes through.
				if sgr := safeSGR(seq); sgr != "" {
					b.WriteString(sgr)
				}
				i += width
				continue
			}
			// A lone escape, or something unparsable: drop the byte.
			i++
			continue
		}

		switch {
		case c == '\t':
			b.WriteString("    ")
			i++
		case c < 0x20 || c == 0x7f:
			// C0 and delete: nothing here should be able to move the cursor.
			i++
		case c == 0xc2 && i+1 < len(s) && s[i+1] >= 0x80 && s[i+1] <= 0x9f:
			// C1 controls in their two-byte UTF-8 form, which includes the
			// eight-bit CSI and OSC introducers.
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}

	return b.String()
}

// readEscape measures one escape sequence starting at the escape byte,
// reporting its text and length.
func readEscape(s string) (string, int, bool) {
	if len(s) < 2 {
		return "", 0, false
	}

	switch s[1] {
	case '[':
		// CSI: parameters, then a final byte in the @ to ~ range.
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return s[:i+1], i + 1, true
			}
		}
		return "", 0, false

	case ']':
		// OSC: runs to a bell or a string terminator. Never kept, but it has
		// to be measured to be skipped whole.
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return s[:i+1], i + 1, true
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return s[:i+2], i + 2, true
			}
		}
		return "", 0, false

	case 'P', 'X', '^', '_':
		// DCS, SOS, PM and APC, all terminated by a string terminator.
		for i := 2; i < len(s); i++ {
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return s[:i+2], i + 2, true
			}
		}
		return "", 0, false

	default:
		// Two-byte escapes such as charset selection or RIS.
		return s[:2], 2, true
	}
}

// safeSGR returns the sequence when it is a colour change, and nothing
// otherwise. The parameters are checked one by one: a CSI that ends in m can
// still carry values worth refusing.
func safeSGR(seq string) string {
	if len(seq) < 3 || seq[1] != '[' || seq[len(seq)-1] != 'm' {
		return ""
	}

	params := seq[2 : len(seq)-1]
	if params == "" {
		return "\x1b[0m"
	}
	// Private parameter bytes have no business in an SGR sequence.
	for i := 0; i < len(params); i++ {
		if params[i] != ';' && params[i] != ':' && (params[i] < '0' || params[i] > '9') {
			return ""
		}
	}
	return seq
}

// SanitizeWidth sanitises text and cuts it to a display width, which is what
// every cell of every table needs.
func SanitizeWidth(s string, width int) string {
	clean := Sanitize(s)
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(clean) <= width {
		return clean
	}
	return ansi.Truncate(clean, width, "")
}
