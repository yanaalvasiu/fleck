// SPDX-License-Identifier: GPL-3.0-or-later
package cache

import (
	"testing"
	"time"
)

func TestSaveLoad(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, "fleck", Stats{Module: "clean", Items: 3, Bytes: 42}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home, "fleck", "clean")
	if err != nil {
		t.Fatal(err)
	}
	if got.Items != 3 || got.Bytes != 42 || got.Module != "clean" || got.LastRun.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("bad stats: %#v", got)
	}
}
