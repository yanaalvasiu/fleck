// SPDX-License-Identifier: GPL-3.0-or-later
//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yanaalvasiu/fleck/internal/config"
	"github.com/yanaalvasiu/fleck/internal/output"
	"github.com/yanaalvasiu/fleck/internal/safety"
)

func TestDarwinUnloadLaunchAgentUsesLaunchctlFixture(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	launchctl := filepath.Join(dir, "launchctl")
	script := "#!/bin/sh\nprintf '%s' \"$*\" > \"" + marker + "\"\n"
	if err := os.WriteFile(launchctl, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	plist := filepath.Join(t.TempDir(), "com.example.demo.plist")
	if err := os.WriteFile(plist, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unloadLaunchAgent(plist); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "bootout gui/") || !strings.Contains(string(b), plist) {
		t.Fatalf("launchctl args = %q", b)
	}
}

func TestDarwinCloneDuplicateKeepsTargetAndUsesCloneMode(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Library", "Caches", "Fixture")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.txt")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(source, []byte("clone fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := safety.NewPolicy(home, []string{filepath.Join(home, "Library")}, config.Config{}.Whitelist)
	if err != nil {
		t.Fatal(err)
	}
	if err := cloneDuplicate(source, target, p, output.Options{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "clone fixture" {
		t.Fatalf("target = %q", b)
	}
}
