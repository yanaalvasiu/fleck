// SPDX-License-Identifier: GPL-3.0-or-later
package permissions

import (
	"fmt"
	"os"
	"runtime"
)

// Status is advisory: it reports access problems without attempting elevation.
func Status() string {
	if runtime.GOOS == "windows" {
		return "not applicable on Windows"
	}
	if _, err := os.ReadDir("/Library/Caches"); err != nil {
		return fmt.Sprintf("system caches unavailable (%v); grant Full Disk Access to Fleck or Terminal", err)
	}
	return "system cache locations readable"
}
