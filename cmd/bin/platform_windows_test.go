// SPDX-License-Identifier: GPL-3.0-or-later
//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// These fixtures model the stable CSV/table contracts of tasklist and
// netstat. They avoid depending on the host's process table or open ports.
func TestWindowsProcessListingParsesFixtureAndFilters(t *testing.T) {
	installWindowsCommandFixtures(t, map[string]string{
		"tasklist.cmd": "@echo off\necho \"fleck-fixture.exe\",\"4242\",\"Console\",\"1\",\"12345 K\"\necho \"other.exe\",\"5252\",\"Console\",\"1\",\"23456 K\"\n",
	})

	rows, err := listProcessesWindows("fleck")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].PID != 4242 || rows[0].Command != "fleck-fixture.exe" || rows[0].Memory != "12345 K" {
		t.Fatalf("process rows = %#v", rows)
	}
}

func TestWindowsPortListingParsesFixtureAndResolvesOwner(t *testing.T) {
	installWindowsCommandFixtures(t, map[string]string{
		"tasklist.cmd": "@echo off\necho \"fleck-fixture.exe\",\"4242\",\"Console\",\"1\",\"12345 K\"\n",
		"netstat.cmd":  "@echo off\necho Active Connections\necho Proto Local Address Foreign Address State PID\necho TCP 0.0.0.0:3003 0.0.0.0:0 LISTENING 4242\n",
	})

	rows, err := listWindowsPorts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Port != "3003" || rows[0].PID != "4242" || rows[0].Command != "fleck-fixture.exe" {
		t.Fatalf("port rows = %#v", rows)
	}
}

func installWindowsCommandFixtures(t *testing.T, commands map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range commands {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+";"+os.Getenv("PATH"))
}
