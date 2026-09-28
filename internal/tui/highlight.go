package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// mdHighlight renders assistant text: fenced code blocks are syntax
// highlighted with chroma (dark-friendly), everything else passes through.
func mdHighlight(text string) string {
	var out strings.Builder
	lines := strings.Split(text, "\n")
	inCode := false
	var buf strings.Builder
	lang := ""
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "```") {
			if !inCode {
				inCode = true
				lang = strings.TrimPrefix(trimmed, "```")
				buf.Reset()
				continue
			}
			// closing fence
			out.WriteString(highlightCode(lang, buf.String()))
			inCode = false
			continue
		}
		if inCode {
			buf.WriteString(ln + "\n")
		} else {
			out.WriteString(ln + "\n")
		}
	}
	if inCode { // unclosed fence: highlight what we have
		out.WriteString(highlightCode(lang, buf.String()))
	}
	return strings.TrimRight(out.String(), "\n")
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
