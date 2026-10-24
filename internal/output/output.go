// SPDX-License-Identifier: GPL-3.0-or-later
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Options struct{ JSON, Yes, DryRun, Debug, NoColor bool }

func Print(v any, o Options) error {
	if o.JSON {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	if IsTTY() {
		PrintTerminal(fmt.Sprintln(v))
	} else {
		fmt.Println(v)
	}
	return nil
}
func IsTTY() bool { fi, err := os.Stdout.Stat(); return err == nil && fi.Mode()&os.ModeCharDevice != 0 }

// NormalizeTerminalNewlines makes line-oriented output safe while the terminal
// is in raw mode. Raw mode disables the driver's LF-to-CRLF translation, so a
// lone LF advances the cursor without returning it to column zero. Existing
// CRLF and progress-line CRs are preserved.
func NormalizeTerminalNewlines(text string) string {
	if !strings.Contains(text, "\n") {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + strings.Count(text, "\n"))
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' && (i == 0 || text[i-1] != '\r') {
			b.WriteByte('\r')
		}
		b.WriteByte(text[i])
	}
	return b.String()
}

// WriteTerminal writes terminal UI text with safe line endings. It is also
// suitable for normal TTY output; non-terminal output should use fmt directly
// so pipes and JSON retain their conventional LF format.
func WriteTerminal(w io.Writer, text string) error {
	_, err := io.WriteString(w, NormalizeTerminalNewlines(text))
	return err
}

func PrintTerminal(text string) {
	if IsTTY() {
		_ = WriteTerminal(os.Stdout, text)
		return
	}
	_, _ = io.WriteString(os.Stdout, text)
}

func PrintfTerminal(format string, args ...any) { PrintTerminal(fmt.Sprintf(format, args...)) }

func PrintlnTerminal(args ...any) { PrintTerminal(fmt.Sprintln(args...)) }
