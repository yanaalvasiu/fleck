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

func TestGroupCleanItemsAggregatesBundleFamilies(t *testing.T) {
	groups := groupCleanItems([]cleanPlanItem{
		{Name: "com.apple.bird", Bytes: 1024},
		{Name: "com.apple.parsecd", Bytes: 2048},
		{Name: "go-build", Bytes: 4096},
	})
	if len(groups) != 2 {
		t.Fatalf("groupCleanItems() returned %d groups, want 2", len(groups))
	}
	if groups[0].Name != "com.apple" || groups[0].Count != 2 || groups[0].Bytes != 3072 {
		t.Fatalf("com.apple group = %#v, want 2 items and 3072 bytes", groups[0])
	}
	if groups[1].Name != "go-build" || groups[1].Count != 1 {
		t.Fatalf("go-build group = %#v, want one item", groups[1])
	}
}
