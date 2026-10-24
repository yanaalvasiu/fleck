// SPDX-License-Identifier: GPL-3.0-or-later
package modules

import "testing"

func TestAllHasStableCommands(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range All {
		if m.Command == "" || m.Name == "" || m.Description == "" || seen[m.Command] {
			t.Fatalf("invalid or duplicate module descriptor: %#v", m)
		}
		seen[m.Command] = true
	}
	if len(All) != 4 {
		t.Fatalf("module count = %d, want 4", len(All))
	}
}
