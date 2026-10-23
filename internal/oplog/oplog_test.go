// SPDX-License-Identifier: GPL-3.0-or-later
package oplog

import "testing"

func TestJSONL(t *testing.T) {
	h := t.TempDir()
	if err := Append(h, "bin", Entry{Action: "trash", Module: "test", Path: "/tmp/x", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	e, err := Read(h, "bin")
	if err != nil {
		t.Fatal(err)
	}
	if len(e) != 1 || e[0].Path != "/tmp/x" {
		t.Fatalf("unexpected entries: %#v", e)
	}
}
