// SPDX-License-Identifier: GPL-3.0-or-later
package output

import "testing"

func TestNormalizeTerminalNewlinesPreservesCRLFAndProgressCarriageReturns(t *testing.T) {
	got := NormalizeTerminalNewlines("one\ntwo\r\n\rprogress\n")
	want := "one\r\ntwo\r\n\rprogress\r\n"
	if got != want {
		t.Fatalf("normalized output = %q, want %q", got, want)
	}
}

func TestNormalizeTerminalNewlinesLeavesANSIAndTextUntouched(t *testing.T) {
	const text = "\033[38;5;39mVersion\033[0m"
	if got := NormalizeTerminalNewlines(text); got != text {
		t.Fatalf("text changed without newlines: %q", got)
	}
}
