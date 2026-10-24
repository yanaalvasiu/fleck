// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
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
	"github.com/yanaalvasiu/fleck/internal/safety"
	"golang.org/x/term"
)

type portRow struct {
	Port      string `json:"port"`
	PID       string `json:"pid"`
	Command   string `json:"command"`
	User      string `json:"user"`
	Cmdline   string `json:"cmdline,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	StartTime string `json:"start_time,omitempty"`
	CPU       string `json:"cpu,omitempty"`
	Memory    string `json:"memory,omitempty"`
}

func portsCmd(args []string, o output.Options) error {
	if len(args) >= 1 && args[0] == "free" {
		if len(args) < 2 || len(args) > 3 || (len(args) == 3 && args[2] != "--group") {
			return fmt.Errorf("usage: ports free PORT [--group]")
		}
		return freePort(args[1], len(args) == 3, o)
	}
	udp := false
	filteredArgs := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--udp" {
			udp = true
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}
	protocol := "-iTCP"
	state := "-sTCP:LISTEN"
	if udp {
		protocol = "-iUDP"
		state = "-iUDP"
	}
	cmd := exec.Command("lsof", "-nP", protocol, state, "-Fpcun")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("lsof unavailable or no listeners: %w", err)
	}
	rows := parseLsof(out)
	for i := range rows {
		enrichPortRow(&rows[i])
	}
	if len(filteredArgs) == 1 {
		var f []portRow
		for _, r := range rows {
			if r.Port == filteredArgs[0] {
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
func freePort(port string, group bool, o output.Options) error {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-Fpcu").Output()
	if err != nil {
		return fmt.Errorf("lsof unavailable or no listener on %s: %w", port, err)
	}
	rows := parseLsof(out)
	if len(rows) == 0 {
		return fmt.Errorf("nothing listening on %s", port)
	}
	r := rows[0]
	fmt.Printf("%s (pid %s) owns port %s\n", r.Command, r.PID, port)
	if !confirm(o, "Send SIGTERM?") {
		return fmt.Errorf("operation refused")
	}
	pid, _ := strconv.Atoi(r.PID)
	if err := validateProcessTarget(pid, r.Command, r.User); err != nil {
		command, user := processIdentity(pid)
		if command == "" {
			return err
		}
		if err = validateProcessTarget(pid, command, user); err != nil {
			return err
		}
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	if !waitForProcessExit(pid, 3*time.Second) {
		if group {
			if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
				return fmt.Errorf("SIGKILL process group: %w", err)
			}
		} else if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			return fmt.Errorf("SIGKILL: %w", err)
		}
	}
	return nil
}

func enrichPortRow(row *portRow) {
	row.Cmdline, row.CPU, row.Memory, row.StartTime = processStats(row.PID)
	pid, _ := strconv.Atoi(row.PID)
	_, row.User = processIdentity(pid)
	if cwd, err := exec.Command("lsof", "-a", "-p", row.PID, "-d", "cwd", "-Fn").Output(); err == nil {
		for _, line := range strings.Split(string(cwd), "\n") {
			if strings.HasPrefix(line, "n") {
				row.CWD = line[1:]
			}
		}
	}
}

func processStats(pid string) (string, string, string, string) {
	out, err := exec.Command("ps", "-o", "command=,%cpu=,%mem=,lstart=", "-p", pid).Output()
	if err != nil {
		return "", "", "", ""
	}
	fields := strings.Fields(string(out))
	if len(fields) < 7 {
		return strings.TrimSpace(string(out)), "", "", ""
	}
	return strings.Join(fields[:len(fields)-7], " "), fields[len(fields)-7], fields[len(fields)-6], strings.Join(fields[len(fields)-5:], " ")
}

type procRow struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	CPU     string `json:"cpu"`
	Memory  string `json:"memory"`
}

func procsCmd(args []string, o output.Options) error {
	if len(args) > 0 && args[0] == "kill" {
		if len(args) != 2 {
			return fmt.Errorf("usage: fleck procs kill PID")
		}
		pid, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid PID %q", args[1])
		}
		return killProcess(pid, o)
	}
	watch, query := false, ""
	for _, arg := range args {
		if arg == "--watch" {
			watch = true
		} else if query == "" {
			query = arg
		}
	}
	for {
		rows, err := listProcesses(query)
		if err != nil {
			return err
		}
		if err := output.Print(rows, o); err != nil {
			return err
		}
		if !watch || !output.IsTTY() {
			return nil
		}
		time.Sleep(time.Second)
		fmt.Print("\033[H\033[2J")
	}
}

func listProcesses(query string) ([]procRow, error) {
	out, err := exec.Command("ps", "-axo", "pid=,comm=,%cpu=,%mem=").Output()
	if err != nil {
		return nil, err
	}
	var rows []procRow
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) < 4 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		rows = append(rows, procRow{PID: pid, Command: strings.Join(f[1:len(f)-2], " "), CPU: f[len(f)-2], Memory: f[len(f)-1]})
	}
	if query != "" {
		q := strings.ToLower(query)
		filtered := rows[:0]
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Command), q) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	sort.Slice(rows, func(i, j int) bool {
		left, _ := strconv.ParseFloat(rows[i].CPU, 64)
		right, _ := strconv.ParseFloat(rows[j].CPU, 64)
		return left > right
	})
	return rows, s.Err()
}

func killProcess(pid int, o output.Options) error {
	command, user := processIdentity(pid)
	if err := validateProcessTarget(pid, command, user); err != nil {
		return err
	}
	if !confirm(o, fmt.Sprintf("Send SIGTERM to %s (pid %d)?", command, pid)) {
		return fmt.Errorf("operation refused")
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

func validateProcessTarget(pid int, command, user string) error {
	if pid <= 1 || pid == os.Getpid() || pid == os.Getppid() {
		return fmt.Errorf("refusing protected PID %d", pid)
	}
	for _, protected := range []string{"launchd", "kernel_task", "WindowServer"} {
		if strings.EqualFold(command, protected) {
			return fmt.Errorf("refusing protected process %q", command)
		}
	}
	current, _ := userName()
	if user != "" && current != "" && user != current {
		return fmt.Errorf("refusing another user's process")
	}
	return nil
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) != nil
}

func processIdentity(pid int) (string, string) {
	command, _ := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	user, _ := exec.Command("ps", "-o", "user=", "-p", strconv.Itoa(pid)).Output()
	return strings.TrimSpace(string(command)), strings.TrimSpace(string(user))
}

func userName() (string, error) {
	name, err := exec.Command("id", "-un").Output()
	return strings.TrimSpace(string(name)), err
}

type dupGroup struct {
	Hash  string   `json:"hash"`
	Size  int64    `json:"size"`
	Paths []string `json:"paths"`
}

func duplicateCmd(args []string, o output.Options, home string, cfg config.Config) error {
	root := home
	trashAction := false
	for _, arg := range args {
		switch {
		case arg == "--trash":
			trashAction = true
		case !strings.HasPrefix(arg, "--"):
			root = arg
		}
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = filepath.Clean(absolute)
	}
	if absolute, err := filepath.Abs(home); err == nil {
		home = filepath.Clean(absolute)
	}
	if output.IsTTY() && !o.JSON {
		fmt.Printf("Scanning %s for exact duplicates...\n", root)
	}
	groups, err := findDuplicates(root)
	if err != nil {
		return err
	}
	if !trashAction {
		return output.Print(groups, o)
	}
	rel, err := filepath.Rel(home, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("duplicate actions are limited to the current home directory")
	}
	if o.JSON {
		return output.Print(map[string]any{"groups": groups, "action": "trash", "dry_run": o.DryRun}, o)
	}
	if len(groups) == 0 {
		return fmt.Errorf("nothing to do")
	}
	for _, group := range groups {
		keeper, remove := duplicateKeeper(group.Paths)
		fmt.Printf("keep: %s\n", keeper)
		for _, path := range remove {
			fmt.Printf("move: %s\n", path)
		}
	}
	if o.DryRun {
		return nil
	}
	if !confirm(o, "Move duplicate copies to Trash?") {
		return fmt.Errorf("operation refused")
	}
	p, err := newPolicy(home, cfg)
	if err != nil {
		return err
	}
	moved := 0
	for _, group := range groups {
		_, remove := duplicateKeeper(group.Paths)
		for _, path := range remove {
			if err := moveToTrashInternal(home, p, path, "duplicates", o, needsElevation(path), false); err != nil {
				fmt.Printf("failed: %s (%v)\n", path, err)
				continue
			}
			moved++
		}
	}
	fmt.Printf("Moved %d duplicate item(s) to Trash.\n", moved)
	return nil
}

func duplicateKeeper(paths []string) (string, []string) {
	sorted := append([]string(nil), paths...)
	sort.Slice(sorted, func(i, j int) bool {
		if len(sorted[i]) != len(sorted[j]) {
			return len(sorted[i]) < len(sorted[j])
		}
		return sorted[i] < sorted[j]
	})
	if len(sorted) == 0 {
		return "", nil
	}
	return sorted[0], sorted[1:]
}
func findDuplicates(root string) ([]dupGroup, error) {
	bySize := map[int64][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && skipDuplicateDirectory(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		i, e := d.Info()
		if e == nil && !isHardLinked(i) {
			bySize[i.Size()] = append(bySize[i.Size()], p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]dupGroup, 0)
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

func skipDuplicateDirectory(name string) bool {
	lower := strings.ToLower(name)
	return lower == ".git" || lower == "node_modules" || strings.HasSuffix(lower, ".app") || strings.HasSuffix(lower, ".bundle") || strings.HasSuffix(lower, ".photoslibrary") || lower == "backups.backupdb" || lower == ".timemachine"
}

func isHardLinked(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
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
	orphans := false
	for _, arg := range args {
		if arg == "--orphans" {
			orphans = true
		}
	}
	apps, err := discoverApps(home)
	if err != nil {
		return err
	}
	if orphans {
		return output.Print(findOrphanCandidates(home, apps), o)
	}
	if len(apps) == 0 {
		return fmt.Errorf("no removable applications found")
	}
	items := make([]uninstallItem, 0)
	for _, app := range apps {
		items = append(items, uninstallItem{Path: app.Path, Name: app.Name, BundleID: app.BundleID, Kind: "application"})
		for _, leftover := range appLeftovers(home, app.BundleID) {
			items = append(items, uninstallItem{Path: leftover, Name: filepath.Base(leftover), BundleID: app.BundleID, Kind: "leftover"})
		}
	}
	if o.JSON {
		return output.Print(items, o)
	}
	for _, item := range items {
		fmt.Printf("  %s · %s\n", item.Kind, item.Path)
	}
	if o.DryRun {
		return nil
	}
	if !confirm(o, "Move the selected application and leftovers to Trash?") {
		return fmt.Errorf("operation refused")
	}
	roots := []string{filepath.Join(home, "Library")}
	for _, root := range []string{"/Applications", filepath.Join(home, "Applications")} {
		if _, e := os.Stat(root); e == nil {
			roots = append(roots, root)
		}
	}
	p, err := safety.NewPolicy(home, roots, cfg.Whitelist)
	if err != nil {
		return err
	}
	for _, item := range items {
		elevated := needsElevation(item.Path)
		if err := moveToTrashInternal(home, p, item.Path, "uninstall", o, elevated, false); err != nil {
			fmt.Printf("failed: %s (%v)\n", item.Path, err)
			continue
		}
		fmt.Println("moved:", item.Path)
	}
	return nil
}

type uninstallItem struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	BundleID string `json:"bundle_id"`
	Kind     string `json:"kind"`
}

type appInfo struct {
	Path     string
	Name     string
	BundleID string
}

func discoverApps(home string) ([]appInfo, error) {
	return discoverAppsInRoots([]string{"/Applications", filepath.Join(home, "Applications")})
}

func discoverAppsInRoots(roots []string) ([]appInfo, error) {
	var apps []appInfo
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".app") || strings.HasPrefix(entry.Name(), "Apple ") {
				continue
			}
			path := filepath.Join(root, entry.Name())
			bundleID := readBundleID(filepath.Join(path, "Contents", "Info.plist"))
			if bundleID == "" || strings.HasPrefix(bundleID, "com.apple.") {
				continue
			}
			apps = append(apps, appInfo{Path: path, Name: strings.TrimSuffix(entry.Name(), ".app"), BundleID: bundleID})
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Path < apps[j].Path })
	return apps, nil
}

func readBundleID(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	decoder := xml.NewDecoder(bytes.NewReader(b))
	lastKey := ""
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "key" {
				var key string
				if decoder.DecodeElement(&key, &value) == nil {
					lastKey = key
				}
			} else if value.Name.Local == "string" && lastKey == "CFBundleIdentifier" {
				var id string
				if decoder.DecodeElement(&id, &value) == nil {
					return strings.TrimSpace(id)
				}
			}
		}
	}
}

func appLeftovers(home, bundleID string) []string {
	if bundleID == "" {
		return nil
	}
	var out []string
	for _, root := range []string{
		filepath.Join(home, "Library", "Application Support"),
		filepath.Join(home, "Library", "Caches"),
		filepath.Join(home, "Library", "Preferences"),
		filepath.Join(home, "Library", "Containers"),
		filepath.Join(home, "Library", "Group Containers"),
		filepath.Join(home, "Library", "Saved Application State"),
		filepath.Join(home, "Library", "LaunchAgents"),
		filepath.Join(home, "Library", "Logs"),
		filepath.Join(home, "Library", "WebKit"),
		filepath.Join(home, "Library", "HTTPStorages"),
		filepath.Join(home, "Library", "Cookies"),
	} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), ".plist")
			if entry.Name() == bundleID || name == bundleID {
				out = append(out, filepath.Join(root, entry.Name()))
			}
		}
	}
	return out
}

func findOrphanCandidates(home string, apps []appInfo) []uninstallItem {
	installed := make(map[string]bool, len(apps))
	for _, app := range apps {
		installed[app.BundleID] = true
	}
	seen := make(map[string]bool)
	out := make([]uninstallItem, 0)
	for _, root := range []string{filepath.Join(home, "Library", "Application Support"), filepath.Join(home, "Library", "Caches"), filepath.Join(home, "Library", "Preferences")} {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			name := entry.Name()
			bundleID := strings.TrimSuffix(name, ".plist")
			if strings.Contains(bundleID, ".") && !installed[bundleID] && strings.HasPrefix(bundleID, "com.") {
				path := filepath.Join(root, name)
				if !seen[path] {
					seen[path] = true
					out = append(out, uninstallItem{Path: path, Name: name, BundleID: bundleID, Kind: "orphan-candidate"})
				}
			}
		}
	}
	return out
}
func cleanCmd(args []string, o output.Options, home string, cfg config.Config) error {
	adminGranted := false
	if output.IsTTY() && !o.JSON && !o.DryRun {
		continueScan, err := cleanIntro()
		if err != nil {
			return err
		}
		if !continueScan {
			return nil
		}
		if err := requestAdminAccess(); err != nil {
			return err
		}
		adminGranted = true
	}
	p, err := newPolicy(home, cfg)
	if err != nil {
		return err
	}
	if output.IsTTY() && !o.JSON && !o.DryRun {
		fmt.Println("\r\nScanning approved cache locations...")
	}
	systemCandidates, err := safeSystemCleanCandidates()
	if err != nil {
		return err
	}
	if len(systemCandidates) > 0 {
		roots := append([]string{}, p.AllowedRoots...)
		roots = append(roots, "/Library/Caches")
		p, err = safety.NewPolicy(home, roots, cfg.Whitelist)
		if err != nil {
			return err
		}
	}
	candidates, err := safeCleanCandidates(home)
	if err != nil {
		return err
	}
	candidates = append(systemCandidates, candidates...)
	if output.IsTTY() && !o.JSON && !o.DryRun {
		fmt.Printf("✓ Scan complete: %d approved cache location(s) found.\n", len(candidates))
	}
	if o.JSON {
		return output.Print(map[string]any{"items": candidates, "count": len(candidates)}, o)
	}
	if len(candidates) == 0 {
		return fmt.Errorf("nothing to do")
	}
	plan := makeCleanPlan(candidates)
	fmt.Print("\033[H\033[2J")
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
	elevated := adminGranted
	if len(protected) > 0 && !adminGranted {
		elevated, err = requestElevation("Clean", protected, o)
		if err != nil {
			return err
		}
	}
	moved, skipped, failed := 0, 0, 0
	for _, x := range candidates {
		if _, e := p.Validate(x); e != nil {
			fmt.Printf("skip: %s (%v)\n", x, e)
			skipped++
			continue
		}
		useSudo := elevated && needsElevation(x)
		if !useSudo && needsElevation(x) {
			fmt.Println("skip (requires sudo):", x)
			skipped++
			continue
		}
		if e := moveToTrashInternal(home, p, x, "clean", o, useSudo, false); e != nil {
			fmt.Printf("failed: %s (%v)\n", x, e)
			failed++
			continue
		}
		moved++
	}
	fmt.Printf("\nMoved %d item(s) to Trash", moved)
	if skipped > 0 || failed > 0 {
		fmt.Printf("; skipped %d; failed %d", skipped, failed)
	}
	fmt.Println(".")
	if failed > 0 {
		return fmt.Errorf("%d cache item(s) could not be moved to Trash", failed)
	}
	if moved == 0 {
		return fmt.Errorf("no cache items were moved to Trash")
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
	fmt.Printf("  %s›%s Enter to authorize, scan, and clean caches; %sSpace%s to cancel: ", accent, reset, keyStyle, reset)

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

func requestAdminAccess() error {
	fmt.Println("\r\nEnter your credentials:")
	if err := exec.Command("sudo", "-v").Run(); err != nil {
		return fmt.Errorf("administrator access was not granted: %w", err)
	}
	fmt.Println("✓ Admin access granted")
	return nil
}

type cleanPlanItem struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	Name     string `json:"name"`
	Bytes    int64  `json:"bytes"`
}

func makeCleanPlan(paths []string) []cleanPlanItem {
	plan := make([]cleanPlanItem, 0, len(paths))
	for _, path := range paths {
		plan = append(plan, cleanPlanItem{
			Path:     path,
			Category: cleanCategory(path),
			Name:     filepath.Base(path),
			Bytes:    directorySize(path),
		})
	}
	return plan
}

func cleanCategory(path string) string {
	name := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(filepath.Clean(path), "/Library/Caches/") {
		return "System caches"
	}
	switch {
	case strings.Contains(name, "brave"), strings.Contains(name, "chrome"), strings.Contains(name, "firefox"), strings.Contains(name, "edge"):
		return "Browsers"
	case name == "go-build", name == "homebrew", strings.Contains(name, "npm"), strings.Contains(name, "pnpm"), strings.Contains(name, "yarn"), strings.Contains(name, "pip"), strings.Contains(name, "playwright"):
		return "Developer tools"
	case strings.HasPrefix(name, "com.apple."):
		return "App caches"
	default:
		return "User essentials"
	}
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
	groups := map[string][]cleanPlanItem{}
	for _, item := range plan {
		groups[item.Category] = append(groups[item.Category], item)
	}
	for _, category := range []string{"System caches", "User essentials", "App caches", "Browsers", "Developer tools"} {
		items := groups[category]
		if len(items) == 0 {
			continue
		}
		categoryTotal := sumPlanBytes(items)
		fmt.Printf("%s➤ %s%s · %d items · %s\n", accent, category, reset, len(items), formatBytes(categoryTotal))
		for _, group := range groupCleanItems(items) {
			total += group.Bytes
			if group.Count > 1 {
				fmt.Printf("  %s✓%s %s · %d items · %s\n", green, reset, group.Name, group.Count, formatBytes(group.Bytes))
			} else {
				fmt.Printf("  %s✓%s %-8s %s\n", green, reset, formatBytes(group.Bytes), group.Name)
			}
		}
		fmt.Println()
	}
	fmt.Printf("\n%sTotal reclaimable: %s%s\n", yellow, formatBytes(total), reset)
	if !dryRun {
		fmt.Printf("%sAdministrator access, if needed, will be requested only after confirmation.%s\n", muted, reset)
	}
}

type cleanDisplayGroup struct {
	Name  string
	Count int
	Bytes int64
}

func groupCleanItems(items []cleanPlanItem) []cleanDisplayGroup {
	groups := make([]cleanDisplayGroup, 0, len(items))
	indices := make(map[string]int, len(items))
	for _, item := range items {
		name := cleanFamily(item.Name)
		index, ok := indices[name]
		if !ok {
			indices[name] = len(groups)
			groups = append(groups, cleanDisplayGroup{Name: name})
			index = len(groups) - 1
		}
		groups[index].Count++
		groups[index].Bytes += item.Bytes
	}
	return groups
}

func cleanFamily(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) >= 2 && (parts[0] == "com" || parts[0] == "org" || parts[0] == "net") {
		return parts[0] + "." + parts[1]
	}
	return name
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

// safeSystemCleanCandidates is intentionally limited to direct children of
// /Library/Caches. It does not scan or authorize arbitrary system paths.
func safeSystemCleanCandidates() ([]string, error) {
	const cacheRoot = "/Library/Caches"
	entries, err := os.ReadDir(cacheRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read system cache directory: %w", err)
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
