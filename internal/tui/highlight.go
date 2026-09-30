package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var (
	mdHeading     = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	mdHeadingEnd  = regexp.MustCompile(`\s+#+\s*$`)
	mdList        = regexp.MustCompile(`^([-+*]|[0-9]+[.)])\s+(.+)$`)
	mdCodeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Background(lipgloss.Color("236"))
	mdStrongStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	mdEmStyle     = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("183"))
	mdLinkStyle   = lipgloss.NewStyle().Underline(true).Foreground(lipgloss.Color("81"))
)

// mdHighlight styles prose and fenced code using a dark-terminal palette.
// Incomplete inline markup stays literal until its closing delimiter arrives.
func mdHighlight(text string) string {
	text = safeText(text)
	var out strings.Builder
	lines := strings.Split(text, "\n")
	inCode := false
	var buf strings.Builder
	lang := ""
	fence := ""
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if !inCode && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			fence = trimmed[:3]
			for len(fence) < len(trimmed) && trimmed[len(fence)] == fence[0] {
				fence += string(fence[0])
			}
			inCode = true
			lang = strings.TrimSpace(strings.TrimPrefix(trimmed, fence))
			buf.Reset()
			continue
		}
		if inCode && strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, string(fence[0])) == "" {
			// closing fence
			out.WriteString(highlightCode(lang, buf.String()))
			inCode = false
			continue
		}
		if inCode {
			buf.WriteString(ln + "\n")
		} else {
			out.WriteString(markdownLine(ln) + "\n")
		}
	}
	if inCode { // unclosed fence: highlight what we have
		out.WriteString(highlightCode(lang, buf.String()))
	}
	return strings.TrimRight(out.String(), "\n")
}

func markdownLine(line string) string {
	text := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(text)]
	if m := mdHeading.FindStringSubmatch(text); m != nil {
		return indent + mdStrongStyle.Render(markdownInline(mdHeadingEnd.ReplaceAllString(m[2], ""), 0))
	}
	if text == "---" || text == "***" || text == "___" {
		return indent + dimStyle.Render("────────────")
	}
	if strings.HasPrefix(text, "> ") || text == ">" {
		return indent + dimStyle.Render("│ ") + dimStyle.Render(markdownInline(strings.TrimPrefix(text[1:], " "), 0))
	}
	if m := mdList.FindStringSubmatch(text); m != nil {
		marker := m[1]
		if marker == "-" || marker == "+" || marker == "*" {
			marker = "•"
		}
		return indent + accent.Render(marker) + " " + markdownInline(m[2], 0)
	}
	return markdownInline(line, 0)
}

// Inline spans are parsed before styling so code remains literal and nested
// emphasis can retain its own colours. Bound recursion for untrusted replies.
func markdownInline(text string, depth int) string {
	if depth >= 16 {
		return text
	}
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] == '\\' && i+1 < len(text) && strings.ContainsRune("\\`*_{}[]()#+-.!>~", rune(text[i+1])) {
			out.WriteByte(text[i+1])
			i += 2
			continue
		}
		if text[i] == '`' {
			n := 1
			for i+n < len(text) && text[i+n] == '`' {
				n++
			}
			delim := text[i : i+n]
			if end := strings.Index(text[i+n:], delim); end >= 0 {
				out.WriteString(mdCodeStyle.Render(text[i+n : i+n+end]))
				i += n + end + n
				continue
			}
		}
		if text[i] == '[' {
			if end := strings.Index(text[i+1:], "]("); end >= 0 {
				labelEnd := i + 1 + end
				if close := strings.IndexByte(text[labelEnd+2:], ')'); close >= 0 {
					url := text[labelEnd+2 : labelEnd+2+close]
					out.WriteString(mdLinkStyle.Render(markdownInline(text[i+1:labelEnd], depth+1)))
					out.WriteString(dimStyle.Render(" (" + url + ")"))
					i = labelEnd + 3 + close
					continue
				}
			}
		}
		matched := false
		for _, delim := range []string{"***", "___", "**", "__", "~~", "*", "_"} {
			if !strings.HasPrefix(text[i:], delim) {
				continue
			}
			if delim[0] == '_' && i > 0 {
				r, _ := utf8.DecodeLastRuneInString(text[:i])
				if unicode.IsLetter(r) || unicode.IsNumber(r) {
					continue
				}
			}
			start := i + len(delim)
			end := markdownSpanEnd(text[start:], delim)
			if end <= 0 {
				continue
			}
			content := text[start : start+end]
			if strings.TrimSpace(content) != content {
				continue
			}
			style := mdEmStyle
			if len(delim) >= 2 {
				style = mdStrongStyle
			}
			if len(delim) == 3 {
				style = style.Italic(true)
			}
			if delim == "~~" {
				style = dimStyle.Strikethrough(true)
			}
			out.WriteString(style.Render(markdownInline(content, depth+1)))
			i = start + end + len(delim)
			matched = true
			break
		}
		if !matched {
			out.WriteByte(text[i])
			i++
		}
	}
	return out.String()
}

// Closing emphasis markers inside literal code or escaped text do not count.
func markdownSpanEnd(text, delim string) int {
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' {
			i++
			continue
		}
		if text[i] == '`' {
			n := 1
			for i+n < len(text) && text[i+n] == '`' {
				n++
			}
			if end := strings.Index(text[i+n:], text[i:i+n]); end >= 0 {
				i += n + end + n - 1
				continue
			}
		}
		if strings.HasPrefix(text[i:], delim) {
			return i
		}
	}
	return -1
}

// toolHeader renders the tool header; the payload is syntax highlighted like
// pi does — bash commands through the sh lexer, other args as JSON.
func toolHeader(name, argsJSON string) string {
	var payload string
	var a struct {
		Command string `json:"command"`
	}
	if name == "bash" && json.Unmarshal([]byte(argsJSON), &a) == nil && a.Command != "" {
		payload = chromaText("sh", a.Command)
	} else {
		payload = chromaText("json", argsJSON)
	}
	return fmt.Sprintf("%s %s", accent.Render(safeText(name)), payload)
}

// chromaText applies chroma colours to a single-line snippet (no indent or
// label).
func chromaText(lang, code string) string {
	return strings.TrimSpace(renderCode(lang, code))
}

// renderCode is the shared syntax-highlighting pipeline. Sanitise after JSON
// decoding too, since escaped control characters can occur inside tool arguments.
func renderCode(lang, code string) string {
	code = safeText(code)
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var b bytes.Buffer
	if err := formatter.Format(&b, style, it); err != nil {
		return code
	}
	return b.String()
}

func highlightCode(lang, code string) string {
	rendered := renderCode(lang, code)
	lang = settingsText(lang)
	var out strings.Builder
	if lang != "" {
		out.WriteString(dimStyle.Render("    " + lang))
		out.WriteString("\n")
	}
	for _, ln := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
		out.WriteString("    " + ln + "\n")
	}
	return out.String()
}
