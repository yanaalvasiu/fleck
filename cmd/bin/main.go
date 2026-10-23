// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/yanaalvasiu/fleck/internal/config"
	"github.com/yanaalvasiu/fleck/internal/oplog"
	"github.com/yanaalvasiu/fleck/internal/output"
	"github.com/yanaalvasiu/fleck/internal/safety"
	"github.com/yanaalvasiu/fleck/internal/trash"
)

const binName = "fleck"

var version, commit, buildDate = "dev", "unknown", "unknown"

func main() {
	home, _ := os.UserHomeDir()
	cfg, _ := config.Load(home, binName)
	if len(os.Args) == 1 {
		if output.IsTTY() {
			if err := homeScreen(version, home, cfg); err != nil {
				fail(1, err.Error())
			}
		} else {
			help()
		}
		return
	}
	o := output.Options{}
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--json":
			o.JSON = true
		case "--yes":
			o.Yes = true
		case "--dry-run":
			o.DryRun = true
		case "--debug":
			o.Debug = true
		case "--no-color":
			o.NoColor = true
		case "--help":
			help()
			return
		default:
			fail(2, "unknown flag: "+args[0])
		}
		args = args[1:]
	}
	if len(args) == 0 {
		help()
		return
	}
	if o.JSON || os.Getenv("NO_COLOR") != "" {
		o.NoColor = true
	}
	var err error
	switch args[0] {
	case "version":
		versionCmd(o)
	case "update":
		err = updateCmd(o)
	case "history":
		err = historyCmd(home, o)
	case "ports":
		err = portsCmd(args[1:], o)
	case "procs":
		err = procsCmd(args[1:], o)
	case "duplicates":
		err = duplicateCmd(args[1:], o, home, cfg)
	case "uninstall":
		err = uninstallCmd(args[1:], o, home, cfg)
	case "clean":
		err = cleanCmd(args[1:], o, home, cfg)
	default:
		help()
		fail(2, "unknown command: "+args[0])
	}
	if err != nil {
		if errors.Is(err, safety.ErrDenied) || errors.Is(err, safety.ErrRoot) {
			fail(3, err.Error())
		}
		fail(1, err.Error())
	}
}

func help() {
	fmt.Println(binName + " — a cautious macOS utility tool\n\nUsage:\n  " + binName + " [flags] <command>\n\nCommands:\n  clean        inspect known-safe caches and logs\n  uninstall    inspect applications and exact bundle leftovers\n  duplicates   find exact duplicate files\n  ports        list listening ports; ports free PORT stops its owner\n  procs        list or stop processes\n  history      show append-only operation history\n  update       show or apply a Homebrew update\n  version      show build and update information\n\nFlags: --json --yes --dry-run --debug --no-color")
}
func fail(code int, msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(code) }
func versionCmd(o output.Options) {
	_ = output.Print(map[string]string{"version": version, "commit": commit, "build_date": buildDate, "install_path": os.Args[0], "go": runtime.Version()}, o)
}
func historyCmd(home string, o output.Options) error {
	e, err := oplog.Read(home, binName)
	if err != nil {
		return err
	}
	return output.Print(e, o)
}
func homeScreen(v, home string, cfg config.Config) error {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("enable interactive terminal: %w", err)
	}
	fmt.Print("\033[?25l")
	defer func() {
		_ = term.Restore(fd, state)
		fmt.Print("\033[?25h")
	}()

	selected := 0
	for {
		fmt.Print("\033[H\033[2J")
		fmt.Print(renderHome(v, selected, os.Getenv("NO_COLOR") == ""))

		key, err := readHomeKey(os.Stdin)
		if err != nil {
			return fmt.Errorf("read menu input: %w", err)
		}
		switch key {
		case homeKeyUp:
			selected = (selected + 3) % 4
		case homeKeyDown:
			selected = (selected + 1) % 4
		case homeKeyEnter:
			command := []string{"clean", "uninstall", "duplicates", "ports"}[selected]
			_ = term.Restore(fd, state)
			fmt.Print("\033[H\033[2J")
			if err := runHomeCommand(command, home, cfg); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			state, err = term.MakeRaw(fd)
			if err != nil {
				return fmt.Errorf("restore interactive terminal: %w", err)
			}
			fmt.Print("\nPress any key to return to Fleck.")
			_, _ = readHomeKey(os.Stdin)
		case homeKeyQuit:
			return nil
		}
	}
}

type homeKey byte

const (
	homeKeyUp homeKey = iota + 1
	homeKeyDown
	homeKeyEnter
	homeKeyQuit
)

func readHomeKey(r *os.File) (homeKey, error) {
	var b [1]byte
	if _, err := r.Read(b[:]); err != nil {
		return 0, err
	}
	switch b[0] {
	case 'q', 'Q', 3:
		return homeKeyQuit, nil
	case '\r', '\n':
		return homeKeyEnter, nil
	case 'j':
		return homeKeyDown, nil
	case 'k':
		return homeKeyUp, nil
	case 27:
		var sequence [2]byte
		if _, err := r.Read(sequence[:]); err != nil {
			return 0, err
		}
		if sequence[0] != '[' {
			return 0, nil
		}
		switch sequence[1] {
		case 'A':
			return homeKeyUp, nil
		case 'B':
			return homeKeyDown, nil
		}
	}
	return 0, nil
}

func runHomeCommand(command, home string, cfg config.Config) error {
	o := output.Options{}
	switch command {
	case "clean":
		return cleanCmd(nil, o, home, cfg)
	case "uninstall":
		return uninstallCmd(nil, o, home, cfg)
	case "duplicates":
		return duplicateCmd(nil, o, home, cfg)
	case "ports":
		return portsCmd(nil, o)
	default:
		return fmt.Errorf("unknown menu command: %s", command)
	}
}

func renderHome(v string, selected int, color bool) string {
	reset := ""
	muted := ""
	accent := ""
	green := ""
	if color {
		reset = "\033[0m"
		muted = "\033[38;5;245m"
		accent = "\033[38;5;81m"
		green = "\033[38;5;114m"
	}
	row := func(index, number int, name, description string) string {
		marker := "    "
		start := ""
		labelStyle := ""
		if index == selected {
			marker = "  ▸ "
			labelStyle = accent
			if color {
				start = "\033[48;5;236m"
			}
		}
		return fmt.Sprintf("%s%s%s%-2d  %-17s%s%s%s\n", start, labelStyle, marker, number, name, muted, description, reset)
	}

	return fmt.Sprintf("  %s%s%s  %s%s%s\n"+
		"  %shttps://github.com/yanaalvasiu/fleck%s\n"+
		"  %sA careful toolbox for your Mac.%s\n\n",
		accent, binName, reset, muted, v, reset,
		muted, reset,
		green, reset,
	) +
		row(0, 1, "Clean", "Free up disk space") +
		row(1, 2, "Uninstall", "Remove apps completely") +
		row(2, 3, "Duplicates", "Find repeated files") +
		row(3, 4, "Ports & Procs", "Inspect local processes") +
		"\n" + fmt.Sprintf("  %s↑↓ / j k%s   %sEnter%s open   %sH%s history   %sS%s settings   %sU%s update   %sV%s version   %sQ%s quit\n",
		muted, reset, muted, reset, muted, reset, muted, reset, muted, reset, muted, reset, muted, reset)
}

func newPolicy(home string, cfg config.Config) (safety.Policy, error) {
	roots := cfg.ScanPaths
	if len(roots) == 0 {
		roots = []string{filepath.Join(home, "Library"), filepath.Join(home, "Downloads")}
	}
	return safety.NewPolicy(home, roots, cfg.Whitelist)
}
func confirm(o output.Options, prompt string) bool {
	if o.Yes {
		return true
	}
	fmt.Printf("%s [y/N] ", prompt)
	var s string
	_, _ = fmt.Scanln(&s)
	return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes")
}
func needsElevation(path string) bool {
	return os.Geteuid() != 0 && syscall.Access(path, 2) != nil // POSIX W_OK
}
func requestElevation(module string, paths []string, o output.Options) (bool, error) {
	if len(paths) == 0 || os.Geteuid() == 0 || o.DryRun || o.JSON {
		return false, nil
	}
	fmt.Printf("%s: %d protected item(s) need administrator permission.\n", module, len(paths))
	fmt.Println("Press Enter to continue with sudo, or type s and Enter to skip them.")
	if o.Yes {
		return true, nil
	}
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil && answer != "" {
		return false, err
	}
	if strings.EqualFold(strings.TrimSpace(answer), "s") || strings.EqualFold(strings.TrimSpace(answer), "skip") {
		fmt.Println("Skipped protected items.")
		return false, nil
	}
	return true, nil
}
func moveToTrash(home string, p safety.Policy, path, module string, o output.Options, elevated bool) error {
	resolved, err := p.Validate(path)
	if err != nil {
		return err
	}
	if o.DryRun {
		fmt.Println("would move to Trash:", resolved)
		return nil
	}
	if !confirm(o, "Move listed item(s) to Trash?") {
		return fmt.Errorf("operation refused")
	}
	if elevated {
		err = trash.MoveWithSudo(resolved)
	} else {
		err = trash.Move(resolved)
	}
	if err != nil {
		return err
	}
	return oplog.Append(home, binName, oplog.Entry{Action: "trash", Module: module, Path: resolved, Result: "ok"})
}

func updateCmd(o output.Options) error {
	if !o.Yes {
		fmt.Println("Latest releases are published at https://github.com/yanaalvasiu/fleck/releases")
		fmt.Println("Homebrew install: brew upgrade " + binName)
		return nil
	}
	cmd := "brew upgrade " + binName
	fmt.Println(cmd)
	return nil
}

func _json(v any) string { b, _ := json.Marshal(v); return string(b) }

var _ = http.MethodGet
var _ = strconv.Itoa
var _ = time.Now
