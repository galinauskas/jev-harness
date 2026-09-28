package tui

import (
	"strings"
	"unicode/utf8"
)

// safeText removes terminal control sequences from API, file and config text.
// Newlines and tabs are kept so code and tool output remain readable.
func safeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i++
			if i >= len(s) {
				break
			}
			switch s[i] {
			case '[': // CSI ends at a final byte in [0x40, 0x7e].
				i++
				for i < len(s) {
					c := s[i]
					i++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
			case ']', 'P', '^', '_', 'X': // OSC and string sequences.
				i++
				for i < len(s) {
					if s[i] == 0x07 {
						i++
						break
					}
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default:
				i++
			}
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		if r == '\n' || r == '\t' || (r >= ' ' && r != 0x7f && (r < 0x80 || r > 0x9f) && (r < 0x202a || r > 0x202e) && (r < 0x2066 || r > 0x2069)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
