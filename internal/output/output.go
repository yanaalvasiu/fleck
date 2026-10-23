// SPDX-License-Identifier: GPL-3.0-or-later
package output

import (
	"encoding/json"
	"fmt"
	"os"
)

type Options struct{ JSON, Yes, DryRun, Debug, NoColor bool }

func Print(v any, o Options) error {
	if o.JSON {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	fmt.Println(v)
	return nil
}
func IsTTY() bool { fi, err := os.Stdout.Stat(); return err == nil && fi.Mode()&os.ModeCharDevice != 0 }
