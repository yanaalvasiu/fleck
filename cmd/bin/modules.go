// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yanaalvasiu/fleck/internal/config"
	"github.com/yanaalvasiu/fleck/internal/output"
	"golang.org/x/term"
)

type portRow struct {
	Port    string `json:"port"`
	PID     string `json:"pid"`
	Command string `json:"command"`
	User    string `json:"user"`
}

func portsCmd(args []string, o output.Options) error {
	if len(args) >= 1 && args[0] == "free" {
		if len(args) != 2 {
			return fmt.Errorf("usage: ports free PORT")
		}
		return freePort(args[1], o)
	}
	cmd := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcu")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("lsof unavailable or no listeners: %w", err)
	}
	rows := parseLsof(out)
	if len(args) == 1 {
		var f []portRow
		for _, r := range rows {
			if r.Port == args[0] {
				f = append(f, r)
			}
		}
		rows = f
	}
	return output.Print(rows, o)
}
func parseLsof(b []byte) []portRow {
	var rows []portRow
	var r portRow
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			if r.PID != "" {
				rows = append(rows, r)
			}
			r = portRow{PID: line[1:]}
		case 'c':
			r.Command = line[1:]
		case 'u':
			r.User = line[1:]
		case 'n':
			if i := strings.LastIndex(line, ":"); i >= 0 {
				r.Port = strings.TrimSuffix(line[i+1:], " (LISTEN)")
			}
		}
	}
	if r.PID != "" {
		rows = append(rows, r)
	}
	return rows
}
func freePort(port string, o output.Options) error {
	rows := parseLsof(must(exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-Fpcu").Output()))
	if len(rows) == 0 {
		return fmt.Errorf("nothing listening on %s", port)
	}
	r := rows[0]
	fmt.Printf("%s (pid %s) owns port %s\n", r.Command, r.PID, port)
	if !confirm(o, "Send SIGTERM?") {
		return fmt.Errorf("operation refused")
	}
	pid, _ := strconv.Atoi(r.PID)
	if pid <= 1 {
		return fmt.Errorf("protected PID")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	return nil
}
func must(b []byte, e error) []byte {
	if e != nil {
		return nil
	}
	return b
}

type procRow struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	CPU     string `json:"cpu"`
	Memory  string `json:"memory"`
}

func procsCmd(args []string, o output.Options) error {
	out, err := exec.Command("ps", "-axo", "pid=,comm=,%cpu=,%mem=").Output()
	if err != nil {
		return err
	}
	var rows []procRow
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) < 4 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		rows = append(rows, procRow{PID: pid, Command: strings.Join(f[1:len(f)-2], " "), CPU: f[len(f)-2], Memory: f[len(f)-1]})
	}
	if len(args) > 0 && args[0] != "--watch" {
		q := strings.ToLower(args[0])
		var f []procRow
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.Command), q) {
				f = append(f, r)
			}
		}
		rows = f
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CPU > rows[j].CPU })
	return output.Print(rows, o)
}

type dupGroup struct {
	Hash  string   `json:"hash"`
	Size  int64    `json:"size"`
	Paths []string `json:"paths"`
}

func duplicateCmd(args []string, o output.Options, home string, cfg config.Config) error {
	root := home
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		root = args[0]
	}
	groups, err := findDuplicates(root)
	if err != nil {
		return err
	}
	return output.Print(groups, o)
}
func findDuplicates(root string) ([]dupGroup, error) {
	bySize := map[int64][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		i, e := d.Info()
		if e == nil && i.Size() > 0 {
			bySize[i.Size()] = append(bySize[i.Size()], p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []dupGroup
	for size, paths := range bySize {
		if len(paths) < 2 {
			continue
		}
		by := map[string][]string{}
		for _, p := range paths {
			h, e := hashFile(p)
			if e == nil {
				by[h] = append(by[h], p)
			}
		}
		for h, ps := range by {
			if len(ps) > 1 {
				out = append(out, dupGroup{Hash: h, Size: size, Paths: ps})
			}
		}
	}
	return out, nil
}
func hashFile(p string) (string, error) {
	b, e := os.ReadFile(p)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func uninstallCmd(args []string, o output.Options, home string, cfg config.Config) error {
	fmt.Println("Uninstall discovery is allow-list based and excludes protected Apple apps.")
	protectedRoots := []string{"/Applications", "/Library/Application Support", "/Library/Preferences"}
	var protected []string
	for _, root := range protectedRoots {
		if _, err := os.Stat(root); err == nil && needsElevation(root) {
			protected = append(protected, root)
		}
	}
	if _, err := requestElevation("Uninstall", protected, o); err != nil {
		return err
	}
	return nil
}
func cleanCmd(args []string, o output.Options, home string, cfg config.Config) error {
	if output.IsTTY() && !o.JSON && !o.DryRun {
		continueScan, err := cleanIntro()
		if err != nil {
			return err
		}
		if !continueScan {
			return nil
		}
	}
	p, err := newPolicy(home, cfg)
	if err != nil {
		return err
	}
	candidates, err := safeCleanCandidates(home)
	if err != nil {
		return err
	}
	if o.JSON {
		return output.Print(map[string]any{"items": candidates, "count": len(candidates)}, o)
	}
	if len(candidates) == 0 {
		return fmt.Errorf("nothing to do")
	}
	plan := makeCleanPlan(candidates)
	printCleanPlan(plan, o.DryRun, len(cfg.Whitelist))
	if o.DryRun {
		return nil
	}
	if !confirm(o, "Move the items in this plan to Trash?") {
		return fmt.Errorf("operation refused")
	}
	var protected []string
	for _, x := range candidates {
		if needsElevation(x) {
			protected = append(protected, x)
		}
	}
	elevated, err := requestElevation("Clean", protected, o)
	if err != nil {
		return err
	}
	for _, x := range candidates {
		if _, e := p.Validate(x); e == nil {
			useSudo := elevated && needsElevation(x)
			if !useSudo && needsElevation(x) {
				fmt.Println("skip (requires sudo):", x)
				continue
			}
			if e := moveToTrash(home, p, x, "clean", o, useSudo); e != nil {
				return e
			}
		}
	}
	return nil
}

func cleanIntro() (bool, error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return false, fmt.Errorf("enable clean prompt: %w", err)
	}
	defer func() { _ = term.Restore(fd, state) }()

	reset, title, muted, accent, keyStyle := "", "", "", "", ""
	if os.Getenv("NO_COLOR") == "" {
		reset = "\033[0m"
		title = "\033[1;38;5;141m"
		muted = "\033[38;5;245m"
		accent = "\033[38;5;81m"
		keyStyle = "\033[1;38;5;117m"
	}

	fmt.Print("\033[H\033[2J")
	fmt.Printf("  %sClean Your Mac%s\r\n\r\n", title, reset)
	fmt.Printf("  %sⓘ Use --dry-run to preview, --whitelist to manage protected paths%s\r\n", muted, reset)
	fmt.Printf("  %s›%s Protected items may require administrator access. %sEnter%s continue, %sSpace%s skip: ", accent, reset, keyStyle, reset, keyStyle, reset)

	var key [1]byte
	for {
		if _, err := os.Stdin.Read(key[:]); err != nil {
			return false, fmt.Errorf("read clean prompt: %w", err)
		}
		switch key[0] {
		case ' ', 'q', 'Q', 3:
			fmt.Print("skip\r\n")
			return false, nil
		case '\r', '\n':
			fmt.Print("continue\r\n")
			return true, nil
		}
	}
}

type cleanPlanItem struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	Bytes    int64  `json:"bytes"`
}

func makeCleanPlan(paths []string) []cleanPlanItem {
	plan := make([]cleanPlanItem, 0, len(paths))
	for _, path := range paths {
		plan = append(plan, cleanPlanItem{
			Path:     path,
			Category: "User caches",
			Bytes:    directorySize(path),
		})
	}
	return plan
}

func directorySize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func printCleanPlan(plan []cleanPlanItem, dryRun bool, whitelistCount int) {
	total := int64(0)
	reset, title, muted, accent, green, yellow := "", "", "", "", "", ""
	if os.Getenv("NO_COLOR") == "" {
		reset = "\033[0m"
		title = "\033[1;38;5;141m"
		muted = "\033[38;5;245m"
		accent = "\033[1;38;5;81m"
		green = "\033[38;5;114m"
		yellow = "\033[38;5;221m"
	}

	fmt.Printf("%sClean Your Mac%s\n\n", title, reset)
	fmt.Printf("%sⓘ Preview first. Confirm before anything is moved to Trash.%s\n", muted, reset)
	if dryRun {
		fmt.Printf("%sⓘ Dry run: no files will be changed.%s\n", muted, reset)
	}
	fmt.Printf("%s⚙%s User home | %sWhitelist: %d configured entries%s\n\n", muted, reset, muted, whitelistCount, reset)
	fmt.Printf("%s➤ User caches%s\n", accent, reset)
	for _, item := range plan {
		total += item.Bytes
		fmt.Printf("  %s✓%s %-8s %s\n", green, reset, formatBytes(item.Bytes), item.Path)
	}
	fmt.Printf("\n%sTotal reclaimable: %s%s\n", yellow, formatBytes(total), reset)
	if !dryRun {
		fmt.Printf("%sAdministrator access, if needed, will be requested only after confirmation.%s\n", muted, reset)
	}
}

func sumPlanBytes(plan []cleanPlanItem) int64 {
	var total int64
	for _, item := range plan {
		total += item.Bytes
	}
	return total
}

func formatBytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(bytes)
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == "TB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", bytes)
}

// safeCleanCandidates deliberately plans only top-level cache containers.
// Recursing through Library/Logs would surface unrelated application logs and
// active developer-tool data, which is not an allow-list decision Fleck can
// prove safe. A future category-specific scan can add explicit log paths.
func safeCleanCandidates(home string) ([]string, error) {
	cacheRoot := filepath.Join(home, "Library", "Caches")
	entries, err := os.ReadDir(cacheRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read user cache directory: %w", err)
	}

	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidates = append(candidates, filepath.Join(cacheRoot, entry.Name()))
	}
	return candidates, nil
}

var _ = time.Now
