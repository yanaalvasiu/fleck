// SPDX-License-Identifier: GPL-3.0-or-later
package modules

// Descriptor is the user-facing contract shared by the home screen and help.
type Descriptor struct {
	Command     string
	Name        string
	Description string
}

var All = []Descriptor{
	{Command: "clean", Name: "Clean", Description: "Free up disk space"},
	{Command: "uninstall", Name: "Uninstall", Description: "Remove apps completely"},
	{Command: "duplicates", Name: "Duplicates", Description: "Find repeated files"},
	{Command: "ports", Name: "Ports & Procs", Description: "Inspect local processes"},
}
