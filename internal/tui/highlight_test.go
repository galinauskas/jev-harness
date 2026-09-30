package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownProse(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"screenshot", "- **Root:** `go.mod`, `README.md`\n  - **`internal/`** – packages", "• Root: go.mod, README.md\n  • internal/ – packages"},
		{"heading", "## Project overview ##\n### C#", "Project overview\nC#"},
		{"emphasis", "**bold** *italic* __strong__ _emphasis_ ***both*** ~~old~~", "bold italic strong emphasis both old"},
		{"literal code", "`**literal**` and ``a ` b``", "**literal** and a ` b"},
		{"nested code", "**use `a**b` here**", "use a**b here"},
		{"escape", `\*literal\* **a \* b**`, "*literal* a * b"},
		{"identifiers", "my_file_name foo_bar_baz", "my_file_name foo_bar_baz"},
		{"partial", "**unfinished and `partial", "**unfinished and `partial"},
		{"quote and list", "> A **note**\n1. First\n---", "│ A note\n1. First\n────────────"},
		{"link", "[docs](https://example.com/docs)", "docs (https://example.com/docs)"},
		{"unicode", "**café** and `世界`", "café and 世界"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ansi.Strip(mdHighlight(tc.input)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMarkdownHasDistinctStyles(t *testing.T) {
	code := mdHighlight("`go.mod`")
	strong := mdHighlight("**Root:**")
	if !strings.Contains(code, "\x1b[") || !strings.Contains(strong, "\x1b[") || code == strong {
		t.Fatalf("missing terminal styling: code %q, strong %q", code, strong)
	}
	if !strings.Contains(code, mdCodeStyle.Render("go.mod")) || !strings.Contains(strong, mdStrongStyle.Render("Root:")) {
		t.Fatal("inline styles not applied")
	}
}

func TestMarkdownFencesAndSanitising(t *testing.T) {
	for _, input := range []string{
		"```text\n**literal**\n```\n**after**",
		"~~~~text\n**literal**\n```inside\n~~~\n~~~~\n**after**",
	} {
		got := ansi.Strip(mdHighlight(input))
		if !strings.Contains(got, "    **literal**") || !strings.HasSuffix(got, "after") {
			t.Fatalf("bad fence rendering: %q", got)
		}
	}
	if got := ansi.Strip(mdHighlight("```go\nvar x = 1")); !strings.Contains(got, "var x = 1") {
		t.Fatal("lost partial code", got)
	}
	got := mdHighlight("**safe**\x1b]52;c;payload\a\x1b[2J")
	if ansi.Strip(got) != "safe" || strings.Contains(got, "payload") || strings.Contains(got, "\x1b[2J") {
		t.Fatalf("unsafe control sequence: %q", got)
	}
}

func TestMarkdownStreamingAndWrapping(t *testing.T) {
	c := safetyChat(t)
	c.pending.WriteString("- **Root:** `go.mod` and `README.md`")
	c.refreshVP()
	if got := ansi.Strip(c.vp.View()); strings.Contains(got, "**Root:**") || !strings.Contains(got, "Root:") {
		t.Fatalf("streaming prose not rendered: %q", got)
	}
	c.flushPending()
	if got := ansi.Strip(c.transcript.String()); strings.Contains(got, "**Root:**") || !strings.Contains(got, "Root:") {
		t.Fatalf("completed prose not rendered: %q", got)
	}
	for _, line := range strings.Split(padText(mdHighlight("**A long heading with `inline code` and more words**"), 16), "\n") {
		if lipgloss.Width(line) > 16 {
			t.Fatalf("styled line exceeds terminal: %q", line)
		}
	}
}
