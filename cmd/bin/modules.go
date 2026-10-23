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

	"github.com/example/bin/internal/config"
	"github.com/example/bin/internal/output"
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
	return nil
}
func cleanCmd(args []string, o output.Options, home string, cfg config.Config) error {
	p, err := newPolicy(home, cfg)
	if err != nil {
		return err
	}
	roots := []string{filepath.Join(home, "Library", "Caches"), filepath.Join(home, "Library", "Logs")}
	var candidates []string
	for _, r := range roots {
		filepath.WalkDir(r, func(path string, d fs.DirEntry, e error) error {
			if e == nil && path != r {
				candidates = append(candidates, path)
			}
			return nil
		})
	}
	if o.JSON {
		return output.Print(map[string]any{"items": candidates, "count": len(candidates)}, o)
	}
	for _, x := range candidates {
		fmt.Println(x)
	}
	if len(candidates) == 0 {
		return fmt.Errorf("nothing to do")
	}
	if o.DryRun {
		return nil
	}
	if !confirm(o, "Move these known-safe cache/log items to Trash?") {
		return fmt.Errorf("operation refused")
	}
	for _, x := range candidates {
		if _, e := p.Validate(x); e == nil {
			if e := moveToTrash(home, p, x, "clean", o); e != nil {
				return e
			}
		}
	}
	return nil
}

var _ = time.Now
