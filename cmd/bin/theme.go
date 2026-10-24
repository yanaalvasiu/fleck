// SPDX-License-Identifier: GPL-3.0-or-later
package main

import "os"

// Fleck's terminal palette is intentionally blue, rose, and mint rather than
// the green/purple palette used by other Mac cleaners.
type fleckTheme struct {
	Reset, Muted, Primary, Accent, Success, Warning, Danger string
}

func currentTheme(color bool) fleckTheme {
	if !color || os.Getenv("NO_COLOR") != "" {
		return fleckTheme{}
	}
	return fleckTheme{
		Reset: "\033[0m", Muted: "\033[38;5;146m", Primary: "\033[38;5;39m",
		Accent: "\033[38;5;175m", Success: "\033[38;5;158m",
		Warning: "\033[38;5;220m", Danger: "\033[38;5;203m",
	}
}
