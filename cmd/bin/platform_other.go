// SPDX-License-Identifier: GPL-3.0-or-later
//go:build !windows && !darwin && !linux

package main

import "errors"

func unloadLaunchAgent(path string) error { return errors.New("launch agents are a macOS feature") }
