// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/yanaalvasiu/fleck/internal/config"
	"github.com/yanaalvasiu/fleck/internal/oplog"
	"github.com/yanaalvasiu/fleck/internal/output"
	"github.com/yanaalvasiu/fleck/internal/safety"
	_ "golang.org/x/image/webp"
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
	rows, err := listPorts(udp)
	if err != nil {
		return err
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
	if !o.JSON && output.IsTTY() {
		printPortsTable(rows, udp)
		return nil
	}
	return output.Print(rows, o)
}

func listPorts(udp bool) ([]portRow, error) {
	if runtime.GOOS == "windows" {
		return listWindowsPorts(udp)
	}
	protocol := "-iTCP"
	state := "-sTCP:LISTEN"
	if udp {
		protocol = "-iUDP"
		state = "-iUDP"
	}
	out, err := exec.Command("lsof", "-nP", protocol, state, "-Fpcun").Output()
	if err != nil {
		if len(out) == 0 {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return []portRow{}, nil
			}
		}
		return nil, fmt.Errorf("lsof unavailable or no listeners: %w", err)
	}
	rows := parseLsof(out)
	for i := range rows {
		enrichPortRow(&rows[i])
	}
	return rows, nil
}

func printPortsTable(rows []portRow, udp bool) {
	protocol := "TCP"
	if udp {
		protocol = "UDP"
	}
	t := currentTheme(output.IsTTY())
	line := func(format string, args ...any) {
		text := fmt.Sprintf(format, args...)
		if output.IsTTY() {
			text = strings.ReplaceAll(text, "\n", "\r\n")
		}
		fmt.Print(text)
	}
	line("%sPorts · %s listening%s\n\n", t.Primary, protocol, t.Reset)
	if len(rows) == 0 {
		line("  %sNo listening ports found.%s\n", t.Muted, t.Reset)
		return
	}
	line("  %s%-7s %-7s %-22s %-14s %7s %7s%s\n", t.Muted, "PORT", "PID", "PROCESS", "USER", "CPU", "MEM", t.Reset)
	for _, row := range rows {
		line("  %s%-7s%s %-7s %-22s %-14s %7s %7s\n",
			t.Accent, truncateText(row.Port, 7), t.Reset, truncateText(row.PID, 7), truncateText(row.Command, 22), truncateText(row.User, 14), truncateText(row.CPU, 7), truncateText(row.Memory, 7))
	}
	line("\n  %sUse: fleck ports PORT · fleck ports free PORT%s\n", t.Muted, t.Reset)
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
	if runtime.GOOS == "windows" {
		return freeWindowsPort(port, group, o)
	}
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-Fpcu").Output()
	if err != nil {
		return fmt.Errorf("lsof unavailable or no listener on %s: %w", port, err)
	}
	rows := parseLsof(out)
	if len(rows) == 0 {
		return fmt.Errorf("nothing listening on %s", port)
	}
	r := rows[0]
	pid, _ := strconv.Atoi(r.PID)
	if err := validateProcessTarget(pid, r.Command, r.User); err != nil {
		return err
	}
	if !o.JSON && output.IsTTY() {
		fmt.Printf("%s (pid %s) owns port %s\n", r.Command, r.PID, port)
	}
	if err := confirmProcessAction(o, "Send SIGTERM?"); err != nil {
		return err
	}
	command, user := processIdentity(pid)
	if err := validateProcessTarget(pid, command, user); err != nil {
		return fmt.Errorf("process changed before signal: %w", err)
	}
	if err := sendProcessSignal(pid, processSignalTerminate); err != nil {
		return err
	}
	if !waitForProcessExit(pid, 3*time.Second) {
		if group {
			if err := sendProcessGroupSignal(pid, processSignalKill); err != nil {
				return fmt.Errorf("SIGKILL process group: %w", err)
			}
		} else if err := sendProcessSignal(pid, processSignalKill); err != nil {
			return fmt.Errorf("SIGKILL: %w", err)
		}
	}
	return printProcessAction(o, "terminated", pid, command)
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
		if o.JSON || !output.IsTTY() {
			if err := output.Print(rows, o); err != nil {
				return err
			}
		} else {
			printProcsTable(rows, query)
		}
		if !watch || !output.IsTTY() {
			return nil
		}
		time.Sleep(time.Second)
		fmt.Print("\033[H\033[2J")
	}
}

func printProcsTable(rows []procRow, query string) {
	t := currentTheme(output.IsTTY())
	line := func(format string, args ...any) {
		text := fmt.Sprintf(format, args...)
		if output.IsTTY() {
			text = strings.ReplaceAll(text, "\n", "\r\n")
		}
		fmt.Print(text)
	}
	title := "Processes"
	if query != "" {
		title += " · " + query
	}
	line("%s%s%s\n\n", t.Primary, title, t.Reset)
	if len(rows) == 0 {
		line("  %sNo matching processes.%s\n", t.Muted, t.Reset)
		return
	}
	visible := rows
	if len(visible) > 24 {
		visible = visible[:24]
	}
	line("  %s%-8s %-28s %8s %8s%s\n", t.Muted, "PID", "PROCESS", "CPU", "MEM", t.Reset)
	for _, row := range visible {
		line("  %s%-8d%s %-28s %8s %8s\n", t.Accent, row.PID, t.Reset, truncateText(row.Command, 28), truncateText(row.CPU, 8), truncateText(row.Memory, 8))
	}
	if len(visible) < len(rows) {
		line("\n  %s… %d more processes hidden; use --json for the full list.%s\n", t.Muted, len(rows)-len(visible), t.Reset)
	}
	line("\n  %sUse: fleck procs kill PID%s\n", t.Muted, t.Reset)
}

func truncateText(value string, width int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= width {
		return value
	}
	if width < 4 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func listProcesses(query string) ([]procRow, error) {
	if runtime.GOOS == "windows" {
		return listProcessesWindows(query)
	}
	out, err := exec.Command("ps", "-ww", "-axo", "pid=,args=,%cpu=,%mem=").Output()
	if err != nil {
		return nil, err
	}
	var rows []procRow
	s := bufio.NewScanner(bytes.NewReader(out))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		lastSpace := strings.LastIndexFunc(line, unicode.IsSpace)
		if lastSpace < 0 {
			continue
		}
		memory := strings.TrimSpace(line[lastSpace:])
		beforeMemory := strings.TrimSpace(line[:lastSpace])
		cpuSpace := strings.LastIndexFunc(beforeMemory, unicode.IsSpace)
		if cpuSpace < 0 {
			continue
		}
		cpu := strings.TrimSpace(beforeMemory[cpuSpace:])
		beforeCPU := strings.TrimSpace(beforeMemory[:cpuSpace])
		pidEnd := strings.IndexFunc(beforeCPU, unicode.IsSpace)
		if pidEnd < 0 {
			continue
		}
		pid, err := strconv.Atoi(beforeCPU[:pidEnd])
		if err != nil {
			continue
		}
		rows = append(rows, procRow{PID: pid, Command: strings.TrimSpace(beforeCPU[pidEnd:]), CPU: cpu, Memory: memory})
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
	if err := confirmProcessAction(o, fmt.Sprintf("Send SIGTERM to %s (pid %d)?", command, pid)); err != nil {
		return err
	}
	currentCommand, currentUser := processIdentity(pid)
	if err := validateProcessTarget(pid, currentCommand, currentUser); err != nil {
		return fmt.Errorf("process changed before signal: %w", err)
	}
	if err := sendProcessSignal(pid, processSignalTerminate); err != nil {
		return err
	}
	return printProcessAction(o, "terminated", pid, currentCommand)
}

func validateProcessTarget(pid int, command, user string) error {
	if pid <= 1 || pid == os.Getpid() || pid == os.Getppid() {
		return fmt.Errorf("refusing protected PID %d", pid)
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("refusing unidentified PID %d", pid)
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

func confirmProcessAction(o output.Options, prompt string) error {
	if o.Yes {
		return nil
	}
	if o.JSON || !output.IsTTY() {
		return fmt.Errorf("refusing destructive process action without --yes in non-interactive mode")
	}
	if !confirm(o, prompt) {
		return fmt.Errorf("operation refused")
	}
	return nil
}

func printProcessAction(o output.Options, action string, pid int, command string) error {
	if !o.JSON {
		return nil
	}
	return output.Print(map[string]any{"action": action, "command": command, "pid": pid}, o)
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !processAlive(pid)
}

type dupGroup struct {
	Hash  string   `json:"hash"`
	Size  int64    `json:"size"`
	Paths []string `json:"paths"`
}

func duplicateCmd(args []string, o output.Options, home string, cfg config.Config) error {
	root := home
	trashAction := false
	cloneAction := false
	similarAction := false
	threshold := 8
	for _, arg := range args {
		switch {
		case arg == "--trash":
			trashAction = true
		case arg == "--clone":
			cloneAction = true
		case arg == "--similar":
			similarAction = true
		case strings.HasPrefix(arg, "--threshold="):
			threshold, _ = strconv.Atoi(strings.TrimPrefix(arg, "--threshold="))
		case !strings.HasPrefix(arg, "--"):
			root = arg
		}
	}
	if similarAction {
		groups, err := findSimilarImages(root, threshold)
		if err != nil {
			return err
		}
		if o.JSON {
			return output.Print(groups, o)
		}
		for _, group := range groups {
			fmt.Printf("similar images · %d files · distance ≤ %d\n", len(group.Paths), group.Distance)
			for _, p := range group.Paths {
				fmt.Println("  ", p)
			}
		}
		if len(groups) == 0 {
			return fmt.Errorf("no similar image groups found")
		}
		fmt.Println("Similar images are never selected automatically; review them before taking action.")
		return nil
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
	if !trashAction && !cloneAction {
		return output.Print(groups, o)
	}
	rel, err := filepath.Rel(home, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("duplicate actions are limited to the current home directory")
	}
	if o.JSON {
		action := "trash"
		if cloneAction {
			action = "clone"
		}
		return output.Print(map[string]any{"groups": groups, "action": action, "dry_run": o.DryRun}, o)
	}
	if len(groups) == 0 {
		return fmt.Errorf("nothing to do")
	}
	for _, group := range groups {
		keeper, remove := duplicateKeeper(group.Paths)
		fmt.Printf("keep: %s\n", keeper)
		for _, path := range remove {
			if cloneAction {
				fmt.Printf("clone: %s ← %s\n", path, keeper)
			} else {
				fmt.Printf("move: %s\n", path)
			}
		}
	}
	if o.DryRun {
		return nil
	}
	prompt := "Move duplicate copies to Trash?"
	if cloneAction {
		prompt = "Replace duplicate copies with APFS clones?"
	}
	if !confirm(o, prompt) {
		return fmt.Errorf("operation refused")
	}
	p, err := newPolicy(home, cfg)
	if err != nil {
		return err
	}
	moved := 0
	for _, group := range groups {
		keeper, remove := duplicateKeeper(group.Paths)
		for _, path := range remove {
			var actionErr error
			if cloneAction {
				actionErr = cloneDuplicate(keeper, path, p, o)
			} else {
				actionErr = moveToTrashInternal(home, p, path, "duplicates", o, needsElevation(path), false)
			}
			if actionErr != nil {
				fmt.Printf("failed: %s (%v)\n", path, actionErr)
				continue
			}
			moved++
		}
	}
	if cloneAction {
		fmt.Printf("Created %d APFS clone(s).\n", moved)
	} else {
		fmt.Printf("Moved %d duplicate item(s) to Trash.\n", moved)
	}
	return nil
}

func cloneDuplicate(source, target string, p safety.Policy, o output.Options) error {
	if _, err := p.Validate(source); err != nil {
		return fmt.Errorf("keeper refused: %w", err)
	}
	resolved, err := p.Validate(target)
	if err != nil {
		return err
	}
	if o.DryRun {
		fmt.Println("would create APFS clone:", resolved)
		return nil
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("APFS clone mode is only available on macOS")
	}
	// macOS cp -c requests clonefile(2) without cgo; the target is revalidated above.
	if out, err := exec.Command("/bin/cp", "-c", source, resolved).CombinedOutput(); err != nil {
		return fmt.Errorf("clonefile: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

type similarGroup struct {
	Distance int      `json:"distance"`
	Paths    []string `json:"paths"`
}

func findSimilarImages(root string, threshold int) ([]similarGroup, error) {
	type imageHash struct {
		path string
		hash uint64
	}
	var images []imageHash
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && skipDuplicateDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".heic" {
			return nil
		}
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".gif" && ext != ".webp" {
			return nil
		}
		h, err := perceptualHash(path)
		if err == nil {
			images = append(images, imageHash{path, h})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var groups []similarGroup
	used := make([]bool, len(images))
	for i := range images {
		if used[i] {
			continue
		}
		paths := []string{images[i].path}
		used[i] = true
		maxDistance := 0
		for j := i + 1; j < len(images); j++ {
			if used[j] {
				continue
			}
			d := bits.OnesCount64(images[i].hash ^ images[j].hash)
			if d <= threshold {
				used[j] = true
				paths = append(paths, images[j].path)
				if d > maxDistance {
					maxDistance = d
				}
			}
		}
		if len(paths) > 1 {
			groups = append(groups, similarGroup{maxDistance, paths})
		}
	}
	return groups, nil
}

func perceptualHash(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}
	gray := image.NewGray(image.Rect(0, 0, 8, 8))
	var total uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			sx := img.Bounds().Min.X + x*img.Bounds().Dx()/8
			sy := img.Bounds().Min.Y + y*img.Bounds().Dy()/8
			c := img.At(sx, sy)
			gray.Set(x, y, c)
			total += uint64(gray.GrayAt(x, y).Y)
		}
	}
	avg := total / 64
	var hash uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if gray.GrayAt(x, y).Y >= uint8(avg) {
				hash |= uint64(1) << uint(y*8+x)
			}
		}
	}
	return hash, nil
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

func hashFile(p string) (string, error) {
	b, e := os.ReadFile(p)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func uninstallCmd(args []string, o output.Options, home string, cfg config.Config) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("uninstall is currently macOS-only; use Windows Settings or winget to remove applications")
	}
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
	if output.IsTTY() && !o.JSON && !o.DryRun {
		apps, err = selectUninstallApps(apps)
		if err != nil {
			return err
		}
		if len(apps) == 0 {
			return fmt.Errorf("no applications selected")
		}
	}
	items := make([]uninstallItem, 0)
	for _, app := range apps {
		if !app.Cask {
			items = append(items, uninstallItem{Path: app.Path, Name: app.Name, BundleID: app.BundleID, Kind: "application"})
		}
		for _, leftover := range appLeftovers(home, app.BundleID) {
			kind := "leftover"
			if filepath.Dir(leftover) == filepath.Join(home, "Library", "LaunchAgents") {
				kind = "launch-agent"
			}
			items = append(items, uninstallItem{Path: leftover, Name: filepath.Base(leftover), BundleID: app.BundleID, Kind: kind})
		}
	}
	if o.JSON {
		return output.Print(map[string]any{"apps": apps, "items": items}, o)
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
		if item.Kind == "launch-agent" {
			if err := unloadLaunchAgent(item.Path); err != nil {
				fmt.Printf("skipped launch agent %s: %v\n", item.Path, err)
				continue
			}
		}
		elevated := needsElevation(item.Path)
		if err := moveToTrashInternal(home, p, item.Path, "uninstall", o, elevated, false); err != nil {
			fmt.Printf("failed: %s (%v)\n", item.Path, err)
			continue
		}
		fmt.Println("moved:", item.Path)
	}
	for _, app := range apps {
		if app.Cask {
			if err := uninstallCask(app.CaskName, o); err != nil {
				fmt.Printf("cask %s: %v\n", app.CaskName, err)
			}
		}
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
	Cask     bool   `json:"cask,omitempty"`
	CaskName string `json:"cask_name,omitempty"`
}

func discoverApps(home string) ([]appInfo, error) {
	apps, err := discoverAppsInRoots([]string{"/Applications", filepath.Join(home, "Applications")})
	if err != nil {
		return nil, err
	}
	return append(apps, discoverCaskApps()...), nil
}

func discoverCaskApps() []appInfo {
	if _, err := exec.LookPath("brew"); err != nil {
		return nil
	}
	out, err := exec.Command("brew", "list", "--cask").Output()
	if err != nil {
		return nil
	}
	var apps []appInfo
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			apps = append(apps, appInfo{Name: name, Cask: true, CaskName: name})
		}
	}
	return apps
}

func selectUninstallApps(apps []appInfo) ([]appInfo, error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	defer func() { _ = term.Restore(fd, state); fmt.Print("\033[?25h\n") }()
	selected := make([]bool, len(apps))
	cursor := 0
	for {
		fmt.Print("\033[H\033[2J\033[?25l")
		fmt.Println("Uninstall · select applications")
		fmt.Println("  ↑↓ move   Space select   Enter continue   Q cancel")
		fmt.Println()
		for i, app := range apps {
			mark := "·"
			if selected[i] {
				mark = "✓"
			}
			pointer := "  "
			if i == cursor {
				pointer = "▸ "
			}
			label := app.Name
			if app.Cask {
				label += " (Homebrew cask)"
			}
			fmt.Printf("%s%s %s\n", pointer, mark, label)
		}
		key, err := readHomeKey(os.Stdin)
		if err != nil {
			return nil, err
		}
		switch key {
		case homeKeyUp:
			cursor = (cursor + len(apps) - 1) % len(apps)
		case homeKeyDown:
			cursor = (cursor + 1) % len(apps)
		case homeKeySpace:
			selected[cursor] = !selected[cursor]
		case homeKeyEnter:
			var out []appInfo
			for i, app := range apps {
				if selected[i] {
					out = append(out, app)
				}
			}
			return out, nil
		case homeKeyQuit:
			return nil, nil
		}
	}
}

func uninstallCask(name string, o output.Options) error {
	if name == "" {
		return nil
	}
	if o.DryRun {
		fmt.Println("would run: brew uninstall --cask", name)
		return nil
	}
	if !o.Yes && !confirm(o, "Uninstall Homebrew cask "+name+"?") {
		return fmt.Errorf("operation refused")
	}
	cmd := exec.Command("brew", "uninstall", "--cask", name)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
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
	destructive := !o.JSON && !o.DryRun
	if destructive && output.IsTTY() {
		continueScan, err := cleanIntro()
		if err != nil {
			return err
		}
		if !continueScan {
			return nil
		}
	}
	var err error
	if destructive && runtime.GOOS != "windows" {
		if err = authorizeCleanBeforeScan(requestAdminAccess); err != nil {
			return err
		}
		adminGranted = true
	}
	freeBefore, _ := freeSpaceBytes(home)
	p, err := newPolicy(home, cfg)
	if err != nil {
		return err
	}
	if output.IsTTY() && !o.JSON && !o.DryRun {
		fmt.Println("\r\nScanning approved cache locations...")
	}
	interactiveScan := output.IsTTY() && !o.JSON && !o.DryRun
	systemCandidates, err := scanCleanRoot("/Library/Caches", "System caches", interactiveScan)
	if err != nil {
		if interactiveScan {
			fmt.Printf("  %s!%s System caches · unavailable (%v)\n", currentTheme(true).Warning, currentTheme(true).Reset, err)
		}
		systemCandidates = nil
	}
	if len(systemCandidates) > 0 {
		roots := append([]string{}, p.AllowedRoots...)
		roots = append(roots, "/Library/Caches")
		p, err = safety.NewPolicy(home, roots, cfg.Whitelist)
		if err != nil {
			return err
		}
	}
	candidates, err := scanUserCleanCandidates(home, interactiveScan)
	if err != nil {
		return err
	}
	candidates = append(systemCandidates, candidates...)
	if interactiveScan {
		t := currentTheme(true)
		fmt.Printf("%s✓ Scan complete%s: %d approved cache location(s) found.\n", t.Success, t.Reset, len(candidates))
		fmt.Println("  Review the grouped cache plan below before anything is moved.")
		time.Sleep(450 * time.Millisecond)
	}
	if o.JSON {
		return output.Print(map[string]any{"items": candidates, "count": len(candidates)}, o)
	}
	if len(candidates) == 0 {
		return fmt.Errorf("nothing to do")
	}
	plan := makeCleanPlan(candidates)
	markRecreatedSinceLastClean(home, plan)
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
		// Authorization happens before scanning, so protected items should only
		// reach this fallback for platforms that do not require sudo up front.
		elevated, err = requestElevation("Clean", protected, o)
		if err != nil {
			return err
		}
	}
	moved, skipped, failed := 0, 0, 0
	trackedBytes := int64(0)
	for index, x := range candidates {
		if _, e := p.Validate(x); e != nil {
			skipped++
			printCleanDeleteProgress(index+1, len(candidates), filepath.Base(x))
			if output.IsTTY() {
				time.Sleep(45 * time.Millisecond)
			}
			continue
		}
		useSudo := elevated && needsElevation(x)
		if !useSudo && needsElevation(x) {
			skipped++
			printCleanDeleteProgress(index+1, len(candidates), filepath.Base(x))
			if output.IsTTY() {
				time.Sleep(45 * time.Millisecond)
			}
			continue
		}
		bytes := directorySize(x)
		if e := moveToTrashInternalAuthorized(home, p, x, "clean", o, useSudo, adminGranted, false); e != nil {
			failed++
			printCleanDeleteProgress(index+1, len(candidates), filepath.Base(x))
			if output.IsTTY() {
				time.Sleep(45 * time.Millisecond)
			}
			continue
		}
		trackedBytes += bytes
		moved++
		printCleanDeleteProgress(index+1, len(candidates), filepath.Base(x))
		if output.IsTTY() {
			time.Sleep(45 * time.Millisecond)
		}
	}
	finishCleanDeleteProgress()
	freeAfter, _ := freeSpaceBytes(home)
	printCleanupSummary(trackedBytes, moved, freeBefore, freeAfter, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("%d cache item(s) could not be moved to Trash", failed)
	}
	if moved == 0 {
		return fmt.Errorf("no cache items were moved to Trash")
	}
	return nil
}

func authorizeCleanBeforeScan(authorize func() error) error {
	if err := authorize(); err != nil {
		return fmt.Errorf("administrator access was not granted before clean scan: %w", err)
	}
	return nil
}

func freeSpaceBytes(path string) (int64, error) {
	out, err := exec.Command("df", "-kP", path).Output()
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("unexpected df output")
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("unexpected df fields")
	}
	kilobytes, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, err
	}
	return kilobytes * 1024, nil
}

func printCleanupSummary(trackedBytes int64, moved int, freeBefore, freeAfter int64, skipped, failed int) {
	separator := strings.Repeat("=", 70)
	reset, muted, green, yellow, cyan, red := cleanColors()
	fmt.Printf("%s%s%s\n", muted, separator, reset)
	fmt.Printf("%sCleanup complete%s\n", green, reset)
	fmt.Printf("%sTracked cleanup: %s%s | %sItems cleaned: %d%s\n", yellow, formatBytes(trackedBytes), reset, cyan, moved, reset)
	if freeBefore > 0 && freeAfter > 0 {
		delta := freeAfter - freeBefore
		deltaColor := green
		if delta < 0 {
			deltaColor = red
		}
		fmt.Printf("%sFree space: %s (%s%s%s)%s\n", cyan, formatBytes(freeAfter), deltaColor, signedBytes(delta), reset, reset)
	} else {
		fmt.Printf("%sFree space: unavailable%s\n", cyan, reset)
	}
	if skipped > 0 || failed > 0 {
		fmt.Printf("%sSkipped: %d%s | %sFailed: %d%s\n", muted, skipped, reset, red, failed, reset)
	}
	fmt.Printf("%s%s%s\n", muted, separator, reset)
}

func cleanColors() (reset, muted, green, yellow, cyan, red string) {
	t := currentTheme(output.IsTTY())
	return t.Reset, t.Muted, t.Success, t.Warning, t.Primary, t.Danger
}

func printCleanDeleteProgress(done, total int, current string) {
	if !output.IsTTY() {
		return
	}
	reset, muted, green, yellow, _, _ := cleanColors()
	width := 28
	filled := 0
	if total > 0 {
		filled = done * width / total
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("·", width-filled)
	fmt.Printf("\r\033[2K%sDeleting%s %-24s %s[%s%s%s] %d/%d", muted, reset, truncateText(current, 24), yellow, green, bar, yellow, done, total)
}

func finishCleanDeleteProgress() {
	if output.IsTTY() {
		fmt.Println()
	}
}

func signedBytes(bytes int64) string {
	if bytes >= 0 {
		return "+" + formatBytes(bytes)
	}
	return "-" + formatBytes(-bytes)
}

func cleanIntro() (bool, error) {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return false, fmt.Errorf("enable clean prompt: %w", err)
	}
	defer func() { _ = term.Restore(fd, state) }()

	t := currentTheme(output.IsTTY())
	reset, title, muted, accent, keyStyle := t.Reset, t.Accent, t.Muted, t.Primary, t.Warning

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
	if runtime.GOOS == "windows" {
		fmt.Println("\r\nWindows administrator access is not required for the approved user cache paths.")
		return nil
	}
	fmt.Println("\r\nEnter your credentials:")
	if err := exec.Command("sudo", "-v").Run(); err != nil {
		return fmt.Errorf("administrator access was not granted: %w", err)
	}
	fmt.Println("✓ Admin access granted")
	return nil
}

type cleanPlanItem struct {
	Path      string `json:"path"`
	Category  string `json:"category"`
	Name      string `json:"name"`
	Bytes     int64  `json:"bytes"`
	Recreated bool   `json:"recreated_since_last_clean,omitempty"`
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

func markRecreatedSinceLastClean(home string, plan []cleanPlanItem) {
	entries, err := oplog.Read(home, binName)
	if err != nil {
		return
	}
	lastClean := make(map[string]time.Time)
	for _, entry := range entries {
		if entry.Action != "trash" || entry.Module != "clean" || entry.Result != "ok" {
			continue
		}
		if previous, ok := lastClean[entry.Path]; !ok || entry.Time.After(previous) {
			lastClean[entry.Path] = entry.Time
		}
	}
	for i := range plan {
		cleanedAt, ok := lastClean[plan[i].Path]
		if !ok {
			continue
		}
		info, err := os.Stat(plan[i].Path)
		if err == nil && info.ModTime().After(cleanedAt) {
			plan[i].Recreated = true
		}
	}
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
	t := currentTheme(output.IsTTY())
	reset, title, muted, accent, green, yellow := t.Reset, t.Accent, t.Muted, t.Primary, t.Success, t.Warning

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
			recreated := ""
			if group.Recreated {
				recreated = " · ↻ recreated since last clean"
			}
			if group.Count > 1 {
				fmt.Printf("  %s✓%s %s · %d items · %s%s\n", green, reset, group.Name, group.Count, formatBytes(group.Bytes), recreated)
			} else {
				fmt.Printf("  %s✓%s %-8s %s%s\n", green, reset, formatBytes(group.Bytes), group.Name, recreated)
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
	Name      string
	Count     int
	Bytes     int64
	Recreated bool
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
		groups[index].Recreated = groups[index].Recreated || item.Recreated
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
	return scanUserCleanCandidates(home, false)
}

func scanUserCleanCandidates(home string, visible bool) ([]string, error) {
	candidates, err := scanCleanRoot(filepath.Join(home, "Library", "Caches"), "", visible)
	if err != nil {
		return nil, err
	}
	// These are explicit, well-known developer/browser cache roots. We never
	// recursively guess under arbitrary home-directory paths.
	known := []struct{ path, category string }{
		{filepath.Join(home, ".npm"), "Developer tools"},
		{filepath.Join(home, "Library", "Caches", "npm"), "Developer tools"},
		{filepath.Join(home, ".cache", "pip"), "Developer tools"},
		{filepath.Join(home, "Library", "Caches", "pip"), "Developer tools"},
		{filepath.Join(home, ".cache", "go-build"), "Developer tools"},
		{filepath.Join(home, "Library", "Caches", "go-build"), "Developer tools"},
		{filepath.Join(home, ".cache", "huggingface"), "Developer tools"},
		{filepath.Join(home, "Library", "Caches", "huggingface"), "Developer tools"},
		{filepath.Join(home, ".pnpm-store"), "Developer tools"},
		{filepath.Join(home, "Library", "pnpm", "store"), "Developer tools"},
		{filepath.Join(home, "Library", "Caches", "pnpm"), "Developer tools"},
		{filepath.Join(home, ".yarn", "berry", "cache"), "Developer tools"},
		{filepath.Join(home, "Library", "Caches", "Yarn"), "Developer tools"},
		{filepath.Join(home, ".gradle", "caches"), "Developer tools"},
		{filepath.Join(home, "Library", "Caches", "Gradle"), "Developer tools"},
		{filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData"), "Developer tools"},
		{filepath.Join(home, "Library", "Developer", "CoreSimulator", "Caches"), "Developer tools"},
		{filepath.Join(home, "Library", "Caches", "Google", "Chrome"), "Browsers"},
		{filepath.Join(home, "Library", "Caches", "BraveSoftware", "Brave-Browser"), "Browsers"},
		{filepath.Join(home, "Library", "Caches", "Microsoft Edge"), "Browsers"},
		{filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "Cache"), "Browsers"},
		{filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser", "Default", "Cache"), "Browsers"},
		{filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "Default", "Cache"), "Browsers"},
		{filepath.Join(home, "Library", "Logs"), "User essentials"},
	}
	// CoreSimulator stores per-device caches below Devices/<id>/data. Only
	// explicit cache directories are added; the device tree itself is never moved.
	devices := filepath.Join(home, "Library", "Developer", "CoreSimulator", "Devices")
	if entries, readErr := os.ReadDir(devices); readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				known = append(known, struct{ path, category string }{
					filepath.Join(devices, entry.Name(), "data", "Library", "Caches"), "Developer tools",
				})
			}
		}
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			known = append(known,
				struct{ path, category string }{filepath.Join(local, "Temp"), "User essentials"},
				struct{ path, category string }{filepath.Join(local, "npm-cache"), "Developer tools"},
				struct{ path, category string }{filepath.Join(local, "pip", "Cache"), "Developer tools"},
				struct{ path, category string }{filepath.Join(local, "Google", "Chrome", "User Data", "Default", "Cache"), "Browsers"},
			)
		}
	}
	for _, item := range known {
		info, statErr := os.Lstat(item.path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		candidates = append(candidates, item.path)
		if visible {
			printCleanScanItem(item.category, item.path, directorySize(item.path), len(candidates), 0)
		}
	}
	return uniquePaths(candidates), nil
}

func scanCleanRoot(root, category string, visible bool) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(root, entry.Name())
		candidates = append(candidates, path)
		if visible {
			label := category
			if label == "" {
				label = cleanCategory(path)
			}
			printCleanScanItem(label, path, directorySize(path), len(candidates), len(entries))
		}
	}
	return candidates, nil
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func printCleanScanItem(category, path string, size int64, done, total int) {
	if !output.IsTTY() {
		return
	}
	t := currentTheme(true)
	progress := ""
	if total > 0 {
		progress = fmt.Sprintf(" %d/%d", done, total)
	}
	fmt.Printf("\r\033[2K  %s›%s scanning %-16s · %-24s · %s%s", t.Primary, t.Reset, category, truncateText(filepath.Base(path), 24), formatBytes(size), progress)
	fmt.Print("\n")
	time.Sleep(35 * time.Millisecond)
}

var _ = time.Now
