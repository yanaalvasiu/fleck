// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yanaalvasiu/fleck/internal/modules"
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

func TestHomeRenderShowsFleckWordmarkAndMenu(t *testing.T) {
	rendered := renderHome("0.1.8", 0, false)
	for _, want := range []string{"__ _", "0.1.8", "Clean", "Ports & Procs", "Enter open"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("home render missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "fleck 0.1.8") {
		t.Fatalf("home render repeats the product name beside the wordmark:\n%s", rendered)
	}
}

func TestPortsPanelTabsShowActiveView(t *testing.T) {
	ports := renderPortsPanelTabs(0, false)
	procs := renderPortsPanelTabs(1, false)
	if !strings.Contains(ports, "▸ Ports") || strings.Contains(ports, "▸ Processes") {
		t.Fatalf("ports tabs = %q", ports)
	}
	if !strings.Contains(procs, "▸ Processes") || strings.Contains(procs, "▸ Ports") {
		t.Fatalf("process tabs = %q", procs)
	}
}

func TestProcessActionsRequireYesWhenNonInteractive(t *testing.T) {
	if err := confirmProcessAction(output.Options{JSON: true}, "unused"); err == nil {
		t.Fatal("JSON process actions must require --yes")
	}
}

func TestHomeRenderUsesArrowOnlyActiveState(t *testing.T) {
	rendered := stripANSI(renderHome("0.1.8", 0, true))
	var activeLine string
	for _, line := range strings.Split(rendered, "\r\n") {
		if strings.Contains(line, "Clean") {
			activeLine = line
			break
		}
	}
	if !strings.HasPrefix(activeLine, "  ▸ 1  Clean") {
		t.Fatalf("active row should use an arrow without changing label styling: %q", activeLine)
	}
	if strings.Contains(activeLine, "▸ 1  ▸") {
		t.Fatalf("active row duplicated its marker: %q", activeLine)
	}
}

func TestHomeRenderNarrowOmitsOverflowingDetails(t *testing.T) {
	rendered := stripANSI(renderHomeAtWidth("0.1.8", 0, false, 30))
	if strings.Contains(rendered, "https://") || strings.Contains(rendered, "Free up disk space") {
		t.Fatalf("narrow home render should omit wide details:\n%s", rendered)
	}
	for _, line := range strings.Split(strings.TrimSuffix(rendered, "\r\n"), "\r\n") {
		if len(line) > 30 {
			t.Fatalf("narrow home line is %d columns wide:\n%s", len(line), rendered)
		}
	}
}

func TestHomeSelectionWrapsWithModuleCount(t *testing.T) {
	if got := previousHomeSelection(0); got != len(modules.All)-1 {
		t.Fatalf("previousHomeSelection(0) = %d, want %d", got, len(modules.All)-1)
	}
	if got := nextHomeSelection(len(modules.All) - 1); got != 0 {
		t.Fatalf("nextHomeSelection(last) = %d, want 0", got)
	}
}

var ansiEscape = regexp.MustCompile(`\\x1b\\[[0-9;]*m`)

func stripANSI(value string) string { return ansiEscape.ReplaceAllString(value, "") }
