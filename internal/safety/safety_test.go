// SPDX-License-Identifier: GPL-3.0-or-later
package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPolicyRejectsRootsAndEscapes(t *testing.T) {
	h := t.TempDir()
	root := filepath.Join(h, "allowed")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := NewPolicy(h, []string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Validate(root); err == nil {
		t.Fatal("allowed root itself must be refused")
	}
	outside := filepath.Join(h, "outside")
	if err := os.WriteFile(outside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Validate(filepath.Join(root, "..", "outside")); err == nil {
		t.Fatal("escape must be refused")
	}
}

func TestPolicyRejectsSymlinkEscapeAndWhitelist(t *testing.T) {
	h := t.TempDir()
	root := filepath.Join(h, "allowed")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(h, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	file := filepath.Join(outside, "x")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := NewPolicy(h, []string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Validate(filepath.Join(link, "x")); err == nil {
		t.Fatal("symlink escape must be refused")
	}
	inside := filepath.Join(root, "keep")
	if err := os.WriteFile(inside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = NewPolicy(h, []string{root}, []string{inside})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Validate(inside); err == nil {
		t.Fatal("whitelisted item must be refused")
	}
}

func TestProtected(t *testing.T) {
	if !IsProtected("/System/Library") || !IsProtected("/usr/bin") || IsProtected("/tmp/safe") {
		t.Fatal("deny list mismatch")
	}
}
