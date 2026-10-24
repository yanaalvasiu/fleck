// SPDX-License-Identifier: GPL-3.0-or-later
//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/yanaalvasiu/fleck/internal/output"
)

type processSignal int

const (
	processSignalProbe processSignal = iota
	processSignalTerminate
	processSignalKill
)

func sendProcessSignal(pid int, signal processSignal) error {
	if signal == processSignalProbe {
		return nil
	}
	if signal == processSignalKill {
		return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F").Run()
	}
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid)).Run()
}

func sendProcessGroupSignal(pid int, signal processSignal) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

func processAlive(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
	return err == nil && !strings.Contains(string(out), "INFO:")
}
func isHardLinked(info os.FileInfo) bool      { return false }
func platformNeedsElevation(path string) bool { return false }
func platformIsElevated() bool                { return false }

func processIdentity(pid int) (string, string) {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return "", ""
	}
	line := strings.TrimSpace(string(out))
	if line == "" || strings.HasPrefix(line, "INFO:") {
		return "", ""
	}
	parts := strings.Split(line, ",")
	if len(parts) == 0 {
		return "", ""
	}
	return strings.Trim(parts[0], "\""), ""
}

func userName() (string, error) {
	name := os.Getenv("USERNAME")
	if name == "" {
		return "", errors.New("USERNAME is not set")
	}
	return name, nil
}

func unloadLaunchAgent(path string) error { return errors.New("launch agents are a macOS feature") }

func listProcessesWindows(query string) ([]procRow, error) {
	out, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil, err
	}
	var rows []procRow
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		name := strings.Trim(parts[0], "\"")
		pid, err := strconv.Atoi(strings.Trim(parts[1], "\""))
		if err != nil {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			continue
		}
		rows = append(rows, procRow{PID: pid, Command: name, CPU: "n/a", Memory: strings.Trim(parts[4], "\"")})
	}
	return rows, nil
}

func listWindowsPorts(udp bool) ([]portRow, error) {
	protocol := "tcp"
	if udp {
		protocol = "udp"
	}
	out, err := exec.Command("netstat", "-ano", "-p", protocol).Output()
	if err != nil {
		return nil, err
	}
	var rows []portRow
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.EqualFold(fields[0], protocol) {
			continue
		}
		local := fields[1]
		pidField := fields[len(fields)-1]
		if protocol == "tcp" && len(fields) < 5 {
			continue
		}
		if protocol == "tcp" && !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		port := local[strings.LastIndex(local, ":")+1:]
		pid, err := strconv.Atoi(pidField)
		if err != nil {
			continue
		}
		command, user := processIdentity(pid)
		rows = append(rows, portRow{Port: port, PID: pidField, Command: command, User: user})
	}
	return rows, nil
}

func freeWindowsPort(port string, group bool, o output.Options) error {
	rows, err := listWindowsPorts(false)
	if err != nil {
		return err
	}
	var owner portRow
	for _, row := range rows {
		if row.Port == port {
			owner = row
			break
		}
	}
	if owner.PID == "" {
		return fmt.Errorf("nothing listening on %s", port)
	}
	pid, err := strconv.Atoi(owner.PID)
	if err != nil {
		return err
	}
	if err := validateProcessTarget(pid, owner.Command, owner.User); err != nil {
		return err
	}
	if !o.JSON && output.IsTTY() {
		fmt.Printf("%s (pid %s) owns port %s\n", owner.Command, owner.PID, port)
	}
	if err := confirmProcessAction(o, "Stop process?"); err != nil {
		return err
	}
	command, user := processIdentity(pid)
	if err := validateProcessTarget(pid, command, user); err != nil {
		return fmt.Errorf("process changed before signal: %w", err)
	}
	if group {
		if err := sendProcessGroupSignal(pid, processSignalKill); err != nil {
			return err
		}
		return printProcessAction(o, "killed", pid, command)
	}
	if err := sendProcessSignal(pid, processSignalTerminate); err != nil {
		return err
	}
	if !waitForProcessExit(pid, 3*time.Second) {
		if err := sendProcessSignal(pid, processSignalKill); err != nil {
			return err
		}
	}
	return printProcessAction(o, "terminated", pid, command)
}
