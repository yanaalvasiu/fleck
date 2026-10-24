// SPDX-License-Identifier: GPL-3.0-or-later
package completions

import "testing"

func TestForShell(t *testing.T) {
	for _, shell := range []string{"zsh", "bash", "fish"} {
		if text, ok := ForShell(shell); !ok || text == "" {
			t.Fatalf("missing %s completion", shell)
		}
	}
	if _, ok := ForShell("powershell"); ok {
		t.Fatal("unexpected unsupported completion")
	}
}
