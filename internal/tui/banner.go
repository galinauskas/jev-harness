package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Version is the displayed build version. Release builds can override it with
// -ldflags "-X jevharness/internal/tui.Version=...".
var Version = "v0.2.0"

var chatMark = []string{
	"     ██ ▄██████ ██   ██ ██   ██ ▄█████▄ ██████▄ ▄█████▄ ▄██████ ▄█████▄ ▄█████▄",
	"     ██ ██▄▄▄▄  ██   ██ ██▄▄▄██ ██▄▄▄██ ██   ██ ██   ██ ██▄▄▄▄  ██▄▄▄▄  ██▄▄▄▄ ",
	"██   ██ ██▀▀▀▀  ██▄ ▄██ ██▀▀▀██ ██▀▀▀██ ██████  ██   ██ ██▀▀▀▀   ▀▀▀▀██  ▀▀▀▀██",
	"▀█████▀ ▀██████  ▀███▀  ██   ██ ██   ██ ██  ▀██ ██   ██ ▀██████ ▀█████▀ ▀█████▀",
}

// chatBanner is part of the scrollable chat content, not a separate screen.
func chatBanner(width int) string {
	if width < 1 {
		width = 80
	}
	markWidth := 0
	for _, line := range chatMark {
		markWidth = max(markWidth, lipgloss.Width(line))
	}
	lines := chatMark
	if width < markWidth+1 {
		lines = []string{"jev-harness"}
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, line := range lines {
		b.WriteByte(' ')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	version := Version
	if version == "" {
		version = "development build"
	}
	b.WriteByte(' ')
	b.WriteString(version)
	b.WriteString("\n\n")
	return b.String()
}
