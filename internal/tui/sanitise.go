package tui

import (
	"regexp"
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

// Saved transcripts may retain colour and emphasis, but no terminal commands.
var transcriptStyle = regexp.MustCompile(`\x1b\[[0-9;:]{0,128}m`)

func safeTranscript(s string) string {
	var b strings.Builder
	pos := 0
	for _, span := range transcriptStyle.FindAllStringIndex(s, -1) {
		b.WriteString(safeText(s[pos:span[0]]))
		b.WriteString(s[span[0]:span[1]])
		pos = span[1]
	}
	b.WriteString(safeText(s[pos:]))
	return b.String()
}
