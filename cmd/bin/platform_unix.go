// SPDX-License-Identifier: GPL-3.0-or-later
//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/yanaalvasiu/fleck/internal/output"
)

type processSignal int

const (
	processSignalProbe processSignal = iota
	processSignalTerminate
	processSignalKill
)

func sendProcessSignal(pid int, signal processSignal) error {
	return syscall.Kill(pid, unixSignal(signal))
}

func sendProcessGroupSignal(pid int, signal processSignal) error {
	return syscall.Kill(-pid, unixSignal(signal))
}

func unixSignal(signal processSignal) syscall.Signal {
	switch signal {
	case processSignalTerminate:
		return syscall.SIGTERM
	case processSignalKill:
		return syscall.SIGKILL
	default:
		return 0
	}
}

func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func isHardLinked(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}

func platformNeedsElevation(path string) bool {
	return os.Geteuid() != 0 && syscall.Access(path, 2) != nil
}
func platformIsElevated() bool { return os.Geteuid() == 0 }

func processIdentity(pid int) (string, string) {
	command, _ := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	user, _ := exec.Command("ps", "-o", "user=", "-p", strconv.Itoa(pid)).Output()
	return strings.TrimSpace(string(command)), strings.TrimSpace(string(user))
}

func userName() (string, error) {
	name, err := exec.Command("id", "-un").Output()
	return strings.TrimSpace(string(name)), err
}

func unloadLaunchAgent(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	uid := strconv.Itoa(os.Getuid())
	if err := exec.Command("launchctl", "bootout", "gui/"+uid, path).Run(); err != nil {
		return err
	}
	return nil
}

func listProcessesWindows(query string) ([]procRow, error) {
	return nil, fmt.Errorf("windows process listing is unavailable on this platform")
}

func listWindowsPorts(udp bool) ([]portRow, error) {
	return nil, fmt.Errorf("windows port listing is unavailable on this platform")
}

func freeWindowsPort(port string, group bool, o output.Options) error {
	return fmt.Errorf("windows port controls are unavailable on this platform")
}
