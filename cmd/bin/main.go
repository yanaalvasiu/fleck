// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

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
	o, args, parseErr := parseArgs(os.Args[1:])
	if parseErr != nil {
		fail(2, parseErr.Error())
	}
	if len(args) == 0 {
		help()
		return
	}
	if o.JSON || os.Getenv("NO_COLOR") != "" {
		o.NoColor = true
	}
	if len(args) > 1 && args[1] == "--help" {
		commandHelp(args[0])
		return
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

func parseArgs(args []string) (output.Options, []string, error) {
	o := output.Options{}
	commands := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
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
			if len(commands) == 0 {
				help()
				os.Exit(0)
			}
			commands = append(commands, arg)
		default:
			commands = append(commands, arg)
		}
	}
	return o, commands, nil
}

func commandHelp(command string) {
	switch command {
	case "clean":
		fmt.Println("Usage: fleck clean [--dry-run] [--yes] [--json]")
	case "uninstall":
		fmt.Println("Usage: fleck uninstall [--orphans] [--dry-run] [--yes] [--json]")
	case "duplicates":
		fmt.Println("Usage: fleck duplicates [PATH] [--dry-run] [--yes] [--json]")
	case "ports":
		fmt.Println("Usage: fleck ports [PORT] | fleck ports free PORT [--yes]")
	case "procs":
		fmt.Println("Usage: fleck procs [SEARCH] [--watch] | fleck procs kill PID [--yes]")
	default:
		help()
	}
}

func help() {
	fmt.Println(binName + " — a cautious macOS utility tool\n\nUsage:\n  " + binName + " [flags] <command>\n\nCommands:\n  clean        inspect known-safe user caches\n  uninstall    inspect applications and exact bundle leftovers\n  duplicates   find exact duplicate files\n  ports        list listening ports; ports free PORT stops its owner\n  procs        list or stop processes\n  history      show append-only operation history\n  update       show or apply a Homebrew update\n  version      show build and update information\n\nFlags: --json --yes --dry-run --debug --no-color")
}
func fail(code int, msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(code) }
func versionCmd(o output.Options) {
	installPath := os.Args[0]
	if executable, err := os.Executable(); err == nil {
		installPath = executable
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			installPath = resolved
		}
	}
	info := map[string]string{"version": version, "commit": commit, "build_date": buildDate, "install_path": installPath, "go": runtime.Version()}
	if o.JSON {
		_ = output.Print(info, o)
		return
	}
	fmt.Printf("%s %s\ncommit: %s\nbuild date: %s\ninstall path: %s\ngo: %s\n", binName, version, commit, buildDate, installPath, runtime.Version())
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
		case homeKeyHistory:
			showHomePanel("History", func() error { return historyCmd(home, output.Options{}) })
		case homeKeySettings:
			showHomePanel("Settings", func() error {
				fmt.Printf("update checks: %v\ncolors: %v\nwhitelist entries: %d\n", cfg.UpdateCheck, cfg.Colors, len(cfg.Whitelist))
				return nil
			})
		case homeKeyUpdate:
			showHomePanel("Update", func() error { return updateCmd(output.Options{}) })
		case homeKeyVersion:
			showHomePanel("Version", func() error { versionCmd(output.Options{}); return nil })
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
	homeKeyHistory
	homeKeySettings
	homeKeyUpdate
	homeKeyVersion
)

func readHomeKey(r *os.File) (homeKey, error) {
	var b [1]byte
	if _, err := r.Read(b[:]); err != nil {
		return 0, err
	}
	switch b[0] {
	case 'q', 'Q', 3:
		return homeKeyQuit, nil
	case 'h', 'H':
		return homeKeyHistory, nil
	case 's', 'S':
		return homeKeySettings, nil
	case 'u', 'U':
		return homeKeyUpdate, nil
	case 'v', 'V':
		return homeKeyVersion, nil
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

func showHomePanel(title string, render func() error) {
	fmt.Print("\033[H\033[2J")
	fmt.Println(title)
	if err := render(); err != nil {
		fmt.Println("error:", err)
	}
	fmt.Print("\nPress any key to return to Fleck.")
	_, _ = readHomeKey(os.Stdin)
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
		return fmt.Sprintf("%s%s%s%-2d  %-17s%s%s%s\r\n", start, labelStyle, marker, number, name, muted, description, reset)
	}

	return fmt.Sprintf("  %s%s%s  %s%s%s\r\n"+
		"  %shttps://github.com/yanaalvasiu/fleck%s\r\n"+
		"  %sA careful toolbox for your Mac.%s\r\n\r\n",
		accent, binName, reset, muted, v, reset,
		muted, reset,
		green, reset,
	) +
		row(0, 1, "Clean", "Free up disk space") +
		row(1, 2, "Uninstall", "Remove apps completely") +
		row(2, 3, "Duplicates", "Find repeated files") +
		row(3, 4, "Ports & Procs", "Inspect local processes") +
		"\r\n" + fmt.Sprintf("  %s↑↓ / j k%s   %sEnter%s open   %sH%s history   %sS%s settings   %sU%s update   %sV%s version   %sQ%s quit\r\n",
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
func moveToTrashInternal(home string, p safety.Policy, path, module string, o output.Options, elevated, askConfirmation bool) error {
	resolved, err := p.Validate(path)
	if err != nil {
		return err
	}
	if o.DryRun {
		fmt.Println("would move to Trash:", resolved)
		return nil
	}
	if askConfirmation && !confirm(o, "Move listed item(s) to Trash?") {
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
	executable, _ := os.Executable()
	resolved := executable
	if real, err := filepath.EvalSymlinks(executable); err == nil {
		resolved = real
	}
	if _, err := exec.LookPath("brew"); err != nil || !strings.Contains(resolved, string(filepath.Separator)+"Cellar"+string(filepath.Separator)) {
		fmt.Println("Fleck was not installed by Homebrew.")
		fmt.Println("Install/update with: brew upgrade " + binName)
		return nil
	}
	cmd := "brew upgrade " + binName
	fmt.Println(cmd)
	if !confirm(o, "Run this command?") {
		return fmt.Errorf("operation refused")
	}
	command := exec.Command("brew", "upgrade", binName)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
