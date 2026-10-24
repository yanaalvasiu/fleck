// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yanaalvasiu/fleck/internal/output"
)

func TestParseArgsAcceptsGlobalFlagsAfterCommand(t *testing.T) {
	o, args, err := parseArgs([]string{"clean", "--dry-run", "--json", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.DryRun || !o.JSON || !o.Yes || len(args) != 1 || args[0] != "clean" {
		t.Fatalf("options=%+v args=%#v", o, args)
	}
}

func TestKillProcessRefusesProtectedPID(t *testing.T) {
	if err := killProcess(1, output.Options{Yes: true}); err == nil {
		t.Fatal("PID 1 must be refused")
	}
}

func TestDiscoverAppsAndExactLeftovers(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "Applications", "Demo.app", "Contents")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0"?><plist><dict><key>CFBundleIdentifier</key><string>com.example.demo</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte(plist), 0o600); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(home, "Library", "Preferences")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "com.example.demo.plist"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	apps, err := discoverAppsInRoots([]string{filepath.Join(home, "Applications")})
	if err != nil || len(apps) != 1 || apps[0].BundleID != "com.example.demo" {
		t.Fatalf("apps=%#v err=%v", apps, err)
	}
	items := appLeftovers(home, apps[0].BundleID)
	if len(items) != 1 || items[0] != filepath.Join(leftover, "com.example.demo.plist") {
		t.Fatalf("leftovers=%#v", items)
	}
}
