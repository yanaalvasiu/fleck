// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yanaalvasiu/fleck/internal/config"
	"github.com/yanaalvasiu/fleck/internal/output"
	"github.com/yanaalvasiu/fleck/internal/safety"
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

func TestCleanMovesCacheToTrash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := filepath.Join(home, "Library", "Caches", "ExampleApp")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	item := filepath.Join(cache, "cache.db")
	if err := os.WriteFile(item, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}

	p, err := safety.NewPolicy(home, []string{filepath.Join(home, "Library")}, config.Config{}.Whitelist)
	if err != nil {
		t.Fatal(err)
	}
	if err := moveToTrashInternal(home, p, cache, "clean", output.Options{}, false, false); err != nil {
		t.Fatalf("moveToTrashInternal() error = %v", err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("cache still exists after move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".Trash", "ExampleApp")); err != nil {
		t.Fatalf("cache was not moved to Trash: %v", err)
	}
}

func TestFindDuplicatesSkipsHardlinksAndIncludesEmptyFiles(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "empty-a")
	second := filepath.Join(root, "empty-b")
	third := filepath.Join(root, "empty-c")
	if err := os.WriteFile(first, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(third, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(root, "hardlink")
	if err := os.Link(first, hardlink); err != nil {
		t.Fatal(err)
	}
	groups, err := findDuplicates(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Paths) != 2 {
		t.Fatalf("duplicate groups = %#v, want only the two independent empty files", groups)
	}
}

func TestDuplicateTrashKeepsOneCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "Library", "Caches", "Demo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "a.txt")
	second := filepath.Join(root, "b.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("same"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := duplicateCmd([]string{"--trash"}, output.Options{Yes: true}, home, config.Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("keeper was removed: %v", err)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatalf("duplicate was not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".Trash", "b.txt")); err != nil {
		t.Fatalf("duplicate is not in Trash: %v", err)
	}
}
