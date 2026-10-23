// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeCleanCandidatesUsesTopLevelCachesOnly(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, "Library", "Caches")
	if err := os.MkdirAll(filepath.Join(cacheRoot, "ExampleApp", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheRoot, "ExampleApp", "nested", "cache.db"), []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Library", "Logs", "unrelated-app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Library", "Logs", "unrelated-app", "app.log"), []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, err := safeCleanCandidates(home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cacheRoot, "ExampleApp")
	if len(items) != 1 || items[0] != want {
		t.Fatalf("safeCleanCandidates() = %#v, want [%q]", items, want)
	}
}
