// SPDX-License-Identifier: GPL-3.0-or-later
//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yanaalvasiu/fleck/internal/output"
)

func TestWindowsPortListingIsNotUsedOnNonWindows(t *testing.T) {
	if _, err := listWindowsPorts(false); err == nil {
		t.Fatal("non-Windows platform unexpectedly exposed Windows port listing")
	}
}

func TestListPortsTreatsNoListenersAsEmpty(t *testing.T) {
	dir := t.TempDir()
	lsof := filepath.Join(dir, "lsof")
	if err := os.WriteFile(lsof, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	rows, err := listPorts(false)
	if err != nil {
		t.Fatal(err)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("rows = %#v, want a non-nil empty result", rows)
	}
}

func TestKillProcessTerminatesSpawnedFixtureOnly(t *testing.T) {
	fixture := exec.Command("sleep", "30")
	if err := fixture.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if fixture.ProcessState == nil || !fixture.ProcessState.Exited() {
			_ = fixture.Process.Kill()
		}
		_ = fixture.Wait()
	}()

	if err := killProcess(fixture.Process.Pid, output.Options{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != -1 {
			t.Fatalf("fixture wait = %v", err)
		}
	}
}
