// SPDX-License-Identifier: GPL-3.0-or-later
//go:build !windows

package main

import "testing"

func TestWindowsPortListingIsNotUsedOnNonWindows(t *testing.T) {
	if _, err := listWindowsPorts(false); err == nil {
		t.Fatal("non-Windows platform unexpectedly exposed Windows port listing")
	}
}
