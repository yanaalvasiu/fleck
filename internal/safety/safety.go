// SPDX-License-Identifier: GPL-3.0-or-later
package safety

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	ErrDenied = errors.New("path refused by safety policy")
	ErrRoot   = errors.New("refusing filesystem root or home directory")
)

// Policy is the allow-list used immediately before every destructive action.
type Policy struct {
	AllowedRoots []string
	Whitelist    []string
}

func NewPolicy(home string, roots []string, whitelist []string) (Policy, error) {
	h, err := filepath.EvalSymlinks(home)
	if err != nil {
		return Policy{}, err
	}
	if len(roots) == 0 {
		roots = []string{h}
	}
	clean := make([]string, 0, len(roots))
	for _, root := range roots {
		r, e := filepath.EvalSymlinks(root)
		if e != nil {
			return Policy{}, e
		}
		if r == "/" || r == h {
			return Policy{}, ErrRoot
		}
		clean = append(clean, r)
	}
	normalizedWhitelist := make([]string, 0, len(whitelist))
	for _, item := range whitelist {
		if resolved, e := filepath.EvalSymlinks(item); e == nil {
			normalizedWhitelist = append(normalizedWhitelist, resolved)
		}
	}
	return Policy{AllowedRoots: clean, Whitelist: normalizedWhitelist}, nil
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func matches(path string, items []string) bool {
	for _, item := range items {
		if path == item || within(path, item) {
			return true
		}
	}
	return false
}

// Validate resolves the current path and its parent, rejecting symlink escapes.
func (p Policy) Validate(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: empty path", ErrDenied)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(abs)
	if clean == "/" {
		return "", ErrRoot
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDenied, err)
	}
	if matches(resolved, p.Whitelist) || IsProtected(resolved) {
		return "", fmt.Errorf("%w: whitelisted or protected path", ErrDenied)
	}
	for _, root := range p.AllowedRoots {
		r, e := filepath.EvalSymlinks(root)
		if e != nil {
			continue
		}
		if resolved == r {
			return "", ErrRoot
		}
		if within(resolved, r) && !matches(resolved, p.Whitelist) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: %s is outside allowed roots or whitelisted", ErrDenied, path)
}

func IsProtected(path string) bool {
	clean := filepath.Clean(path)
	for _, prefix := range []string{"/System", "/usr", "/bin", "/sbin", "/private/etc", "/private/var/db", "/Library/Apple", "/Users/Shared"} {
		if clean == prefix || strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
