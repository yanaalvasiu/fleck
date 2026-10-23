// SPDX-License-Identifier: GPL-3.0-or-later
package config

import "testing"

func TestRoundTrip(t *testing.T) {
	h := t.TempDir()
	c := Config{UpdateCheck: false, Colors: false, ScanPaths: []string{"/tmp/a"}, Whitelist: []string{"/tmp/a/keep"}}
	if err := Save(h, "bin", c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(h, "bin")
	if err != nil {
		t.Fatal(err)
	}
	if got.UpdateCheck || got.Colors || len(got.ScanPaths) != 1 || len(got.Whitelist) != 1 {
		t.Fatalf("unexpected config: %#v", got)
	}
}
