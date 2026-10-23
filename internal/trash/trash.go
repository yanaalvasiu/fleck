// SPDX-License-Identifier: GPL-3.0-or-later
package trash

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Move uses the Finder-compatible ~/.Trash directory. A rename preserves the
// original item for Put Back and avoids invoking shell or AppleScript.
func Move(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	trashDir := filepath.Join(home, ".Trash")
	if err := os.MkdirAll(trashDir, 0700); err != nil {
		return err
	}
	base := filepath.Base(path)
	dest := filepath.Join(trashDir, base)
	if _, err := os.Lstat(dest); err == nil {
		dest = filepath.Join(trashDir, fmt.Sprintf("%s.%d", base, time.Now().UnixNano()))
	}
	if err := os.Rename(path, dest); err == nil {
		return nil
	}
	// Cross-volume moves are copied conservatively, then the source is removed
	// only after all bytes and mode bits have been copied.
	return moveTree(path, dest)
}

func moveTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := moveTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return os.Remove(src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
