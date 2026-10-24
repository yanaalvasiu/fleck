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

	"golang.org/x/term"

	"github.com/yanaalvasiu/fleck/internal/completions"
	"github.com/yanaalvasiu/fleck/internal/config"
	"github.com/yanaalvasiu/fleck/internal/modules"
	"github.com/yanaalvasiu/fleck/internal/oplog"
	"github.com/yanaalvasiu/fleck/internal/output"
	"github.com/yanaalvasiu/fleck/internal/permissions"
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
	case "doctor":
		err = doctorCmd(home, cfg, o)
	case "completion":
		err = completionCmd(args[1:])
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
		fmt.Println("Usage: fleck duplicates [PATH] [--trash|--clone|--similar] [--threshold=N] [--dry-run] [--yes] [--json]")
	case "ports":
		fmt.Println("Usage: fleck ports [PORT] [--udp] | fleck ports free PORT [--group] [--yes]")
	case "procs":
		fmt.Println("Usage: fleck procs [SEARCH] [--watch] | fleck procs kill PID [--yes]")
	case "doctor":
		fmt.Println("Usage: fleck doctor [--json]")
	case "completion":
		fmt.Println("Usage: fleck completion zsh|bash|fish")
	default:
		help()
	}
}

func help() {
	text := binName + " — a cautious macOS utility tool\n\nUsage:\n  " + binName + " [flags] <command>\n\nCommands:\n"
	for _, module := range modules.All {
		text += fmt.Sprintf("  %-12s %s\n", module.Command, module.Description)
	}
	text += "  procs        list or stop processes\n  history      show append-only operation history\n  update       show or apply a Homebrew update\n  version      show build and update information\n  doctor       inspect Fleck permissions and configuration\n  completion   print shell completion\n\nFlags: --json --yes --dry-run --debug --no-color"
	fmt.Println(text)
}
func fail(code int, msg string) { fmt.Fprintln(os.Stderr, msg); os.Exit(code) }
func versionCmd(o output.Options) {
	if o.JSON {
		_ = output.Print(versionInfo(), o)
		return
	}
	output.PrintTerminal(versionText())
}

func versionInfo() map[string]string {
	installPath := os.Args[0]
	if executable, err := os.Executable(); err == nil {
		installPath = executable
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			installPath = resolved
		}
	}
	return map[string]string{"version": version, "commit": commit, "build_date": buildDate, "install_path": installPath, "go": runtime.Version()}
}

func versionText() string {
	info := versionInfo()
	return fmt.Sprintf("%s %s\ncommit: %s\nbuild date: %s\ninstall path: %s\ngo: %s\n", binName, info["version"], info["commit"], info["build_date"], info["install_path"], info["go"])
}
func historyCmd(home string, o output.Options) error {
	e, err := oplog.Read(home, binName)
	if err != nil {
		return err
	}
	return output.Print(e, o)
}

func doctorCmd(home string, cfg config.Config, o output.Options) error {
	checks := map[string]any{
		"home":              home,
		"config_path":       config.Path(home, binName),
		"config_exists":     fileExists(config.Path(home, binName)),
		"whitelist_entries": len(cfg.Whitelist),
		"scan_paths":        cfg.ScanPaths,
		"oplog_path":        filepath.Join(home, "Library", "Logs", binName, "operations.log"),
		"update_check":      cfg.UpdateCheck,
		"trash_exists":      fileExists(filepath.Join(home, ".Trash")),
	}
	checks["full_disk_access"] = permissions.Status()
	if _, err := exec.LookPath("brew"); err == nil {
		checks["homebrew"] = "available"
	} else {
		checks["homebrew"] = "not found"
	}
	if o.JSON {
		return output.Print(checks, o)
	}
	for _, key := range []string{"home", "config_path", "config_exists", "whitelist_entries", "scan_paths", "oplog_path", "trash_exists", "full_disk_access", "homebrew"} {
		fmt.Printf("%-18s %v\n", key+":", checks[key])
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
func homeScreen(v, home string, cfg config.Config) error {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("enable interactive terminal: %w", err)
	}
	output.PrintTerminal("\033[?25l")
	defer func() {
		_ = term.Restore(fd, state)
		output.PrintTerminal("\033[?25h")
	}()

	selected := 0
	for {
		output.PrintTerminal("\033[H\033[2J")
		output.PrintTerminal(renderHomeAtWidth(v, selected, os.Getenv("NO_COLOR") == "", homeTerminalWidth()))

		key, err := readHomeKey(os.Stdin)
		if err != nil {
			return fmt.Errorf("read menu input: %w", err)
		}
		switch key {
		case homeKeyUp:
			selected = previousHomeSelection(selected)
		case homeKeyDown:
			selected = nextHomeSelection(selected)
		case homeKeyEnter:
			command := modules.All[selected].Command
			_ = term.Restore(fd, state)
			output.PrintTerminal("\033[H\033[2J")
			if err := runHomeCommand(command, home, cfg); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			state, err = term.MakeRaw(fd)
			if err != nil {
				return fmt.Errorf("restore interactive terminal: %w", err)
			}
			output.PrintTerminal("\nPress any key to return to Fleck.")
			_, _ = readHomeKey(os.Stdin)
		case homeKeyHistory:
			showHomePanel("History", func() error { return historyCmd(home, output.Options{}) })
		case homeKeySettings:
			showHomePanel("Settings", func() error {
				output.PrintfTerminal("update checks: %v\ncolors: %v\nwhitelist entries: %d\n", cfg.UpdateCheck, cfg.Colors, len(cfg.Whitelist))
				return nil
			})
		case homeKeyUpdate:
			showHomePanel("Update", func() error { return updateCmd(output.Options{}) })
		case homeKeyVersion:
			showHomePanel("Version", func() error { versionCmd(output.Options{}); return nil })
		case homeKeyRefresh:
			// Refresh is handled by the module panel; keep it as a no-op on home.
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
	homeKeyRefresh
	homeKeySpace
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
	case 'r', 'R':
		return homeKeyRefresh, nil
	case '\r', '\n':
		return homeKeyEnter, nil
	case ' ':
		return homeKeySpace, nil
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
	output.PrintTerminal("\033[H\033[2J")
	output.PrintlnTerminal(title)
	if err := render(); err != nil {
		output.PrintlnTerminal("error:", err)
	}
	output.PrintTerminal("\nPress any key to return to Fleck.")
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
		return portsAndProcsPanel()
	default:
		return fmt.Errorf("unknown menu command: %s", command)
	}
}

func portsAndProcsPanel() error {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("enable ports and processes panel: %w", err)
	}
	defer func() {
		_ = term.Restore(fd, state)
		output.PrintTerminal("\033[?25h")
	}()

	selected := 0
	for {
		output.PrintTerminal("\033[H\033[2J")
		output.PrintTerminal(renderPortsPanelTabs(selected, output.IsTTY()))
		if selected == 0 {
			rows, listErr := listPorts(false)
			if listErr != nil {
				output.PrintfTerminal("Ports\n\n  %v\n", listErr)
			} else {
				printPortsTable(rows, false)
			}
		} else {
			rows, listErr := listProcesses("")
			if listErr != nil {
				output.PrintfTerminal("Processes\n\n  %v\n", listErr)
			} else {
				printProcsTable(rows, "")
			}
		}
		output.PrintlnTerminal("  ↑↓ switch view   R refresh   Q return")
		key, readErr := readHomeKey(os.Stdin)
		if readErr != nil {
			return readErr
		}
		switch key {
		case homeKeyUp, homeKeyDown:
			selected = 1 - selected
		case homeKeyQuit, homeKeyEnter:
			return nil
		case homeKeyRefresh:
			continue
		}
	}
}

func renderPortsPanelTabs(selected int, color bool) string {
	theme := currentTheme(color)
	active := theme.Primary
	reset := theme.Reset
	if selected == 0 {
		return fmt.Sprintf("%s▸ Ports%s    Processes\n\n", active, reset)
	}
	return fmt.Sprintf("  Ports    %s▸ Processes%s\n\n", active, reset)
}

func renderHome(v string, selected int, color bool) string {
	return renderHomeAtWidth(v, selected, color, 80)
}

func homeTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 1 {
		return 80
	}
	return width
}

func previousHomeSelection(selected int) int {
	if len(modules.All) == 0 {
		return 0
	}
	return (selected + len(modules.All) - 1) % len(modules.All)
}

func nextHomeSelection(selected int) int {
	if len(modules.All) == 0 {
		return 0
	}
	return (selected + 1) % len(modules.All)
}

func renderHomeAtWidth(v string, selected int, color bool, width int) string {
	theme := currentTheme(color)
	reset, muted, accent, green := theme.Reset, theme.Muted, theme.Primary, theme.Success
	if width < 1 {
		width = 80
	}
	showDetails := width >= 56
	logo := renderBrandHeader(v, showDetails, accent, muted, green, reset)
	row := func(index, number int, name, description string) string {
		marker := "    "
		arrowStyle := ""
		if index == selected {
			marker = "  ▸ "
			arrowStyle = accent
		}
		if arrowStyle != "" {
			marker = "  " + arrowStyle + "▸" + reset + " "
		}
		if !showDetails {
			nameStyle, nameReset := "", ""
			if index == selected {
				nameStyle, nameReset = accent, reset
			}
			return fmt.Sprintf("%s%d  %s%s%s\r\n", marker, number, nameStyle, name, nameReset)
		}
		nameStyle, nameReset := "", ""
		if index == selected {
			nameStyle, nameReset = accent, reset
		}
		return fmt.Sprintf("%s%d  %s%-17s%s%s%s%s\r\n", marker, number, nameStyle, name, nameReset, muted, description, reset)
	}

	// Keep the brand block visually separate from the interactive modules.
	header := logo + "\r\n\r\n"
	footer := fmt.Sprintf("  %s↑↓ / j k%s   %sEnter%s open   %sH%s history   %sS%s settings   %sU%s update   %sV%s version   %sQ%s quit\r\n", muted, reset, muted, reset, muted, reset, muted, reset, muted, reset, muted, reset, muted, reset)
	if !showDetails {
		footer = fmt.Sprintf("  %s↑↓/jk%s · %sEnter%s · %sQ%s\r\n", muted, reset, muted, reset, muted, reset)
	}
	return header + moduleRows(modules.All, row) + "\r\n" + footer
}

func renderBrandHeader(v string, details bool, accent, muted, green, reset string) string {
	if !details {
		return fmt.Sprintf("%s  FLECK%s\r\n  %s%s%s", accent, reset, muted, v, reset)
	}
	mark := []string{
		"  ______ _           _    ",
		" |  ____| |         | |   ",
		" | |__  | | ___  ___| | __",
		" |  __| | |/ _ \\/ __| |/ /",
		" | |    | |  __/ (__|   < ",
		" |_|    |_|\\___|\\___|_|\\_\\",
	}
	right := []string{
		"",
		fmt.Sprintf("%s%s %s%s", muted, binName, v, reset),
		fmt.Sprintf("%shttps://github.com/yanaalvasiu/fleck%s", muted, reset),
		fmt.Sprintf("%sA careful toolbox for your machine.%s", green, reset),
		"",
		"",
	}
	var b strings.Builder
	for i, line := range mark {
		b.WriteString(accent)
		b.WriteString(line)
		b.WriteString(reset)
		b.WriteString("    ")
		b.WriteString(right[i])
		if i+1 < len(mark) {
			b.WriteString("\r\n")
		}
	}
	return b.String()
}

func moduleRows(descriptors []modules.Descriptor, row func(int, int, string, string) string) string {
	var b strings.Builder
	for i, module := range descriptors {
		b.WriteString(row(i, i+1, module.Name, module.Description))
	}
	return b.String()
}

func completionCmd(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: fleck completion zsh|bash|fish")
	}
	data, ok := completions.ForShell(args[0])
	if !ok {
		return fmt.Errorf("unsupported shell %q (choose zsh, bash, or fish)", args[0])
	}
	if output.IsTTY() {
		output.PrintTerminal(data)
	} else {
		fmt.Print(data)
	}
	return nil
}

func newPolicy(home string, cfg config.Config) (safety.Policy, error) {
	roots := cfg.ScanPaths
	if len(roots) == 0 {
		roots = []string{
			filepath.Join(home, "Library"), filepath.Join(home, "Downloads"),
			filepath.Join(home, ".npm"), filepath.Join(home, ".cache"),
			filepath.Join(home, ".pnpm-store"), filepath.Join(home, ".yarn"), filepath.Join(home, ".gradle"),
		}
		if runtime.GOOS == "windows" {
			if local := os.Getenv("LOCALAPPDATA"); local != "" {
				roots = append(roots, filepath.Join(local, "Temp"), filepath.Join(local, "npm-cache"), filepath.Join(local, "pip"), filepath.Join(local, "Google"))
			}
		}
	}
	return safety.NewPolicy(home, roots, cfg.Whitelist)
}
func confirm(o output.Options, prompt string) bool {
	if o.Yes {
		return true
	}
	output.PrintfTerminal("%s [y/N] ", prompt)
	var s string
	_, _ = fmt.Scanln(&s)
	return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes")
}
func needsElevation(path string) bool {
	return platformNeedsElevation(path)
}
func requestElevation(module string, paths []string, o output.Options) (bool, error) {
	if len(paths) == 0 || platformIsElevated() || o.DryRun || o.JSON {
		return false, nil
	}
	output.PrintfTerminal("%s: %d protected item(s) need administrator permission.\n", module, len(paths))
	output.PrintlnTerminal("Press Enter to continue with sudo, or type s and Enter to skip them.")
	if o.Yes {
		return true, nil
	}
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil && answer != "" {
		return false, err
	}
	if strings.EqualFold(strings.TrimSpace(answer), "s") || strings.EqualFold(strings.TrimSpace(answer), "skip") {
		output.PrintlnTerminal("Skipped protected items.")
		return false, nil
	}
	return true, nil
}
func moveToTrashInternal(home string, p safety.Policy, path, module string, o output.Options, elevated, askConfirmation bool) error {
	return moveToTrashInternalAuthorized(home, p, path, module, o, elevated, false, askConfirmation)
}

func moveToTrashInternalAuthorized(home string, p safety.Policy, path, module string, o output.Options, elevated, sudoAuthorized, askConfirmation bool) error {
	resolved, err := p.Validate(path)
	if err != nil {
		return err
	}
	if o.DryRun {
		output.PrintlnTerminal("would move to Trash:", resolved)
		return nil
	}
	if askConfirmation && !confirm(o, "Move listed item(s) to Trash?") {
		return fmt.Errorf("operation refused")
	}
	if elevated {
		if sudoAuthorized {
			err = trash.MoveWithSudoAuthorized(resolved)
		} else {
			err = trash.MoveWithSudo(resolved)
		}
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
		output.PrintlnTerminal("Fleck was not installed by Homebrew.")
		output.PrintlnTerminal("Install/update with: brew upgrade " + binName)
		return nil
	}
	cmd := "brew upgrade " + binName
	output.PrintlnTerminal(cmd)
	if !confirm(o, "Run this command?") {
		return fmt.Errorf("operation refused")
	}
	command := exec.Command("brew", "upgrade", binName)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
