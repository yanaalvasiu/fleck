// SPDX-License-Identifier: GPL-3.0-or-later
package permissions

import "testing"

func TestStatusIsInformative(t *testing.T) {
	if Status() == "" {
		t.Fatal("permission status is empty")
	}
}
