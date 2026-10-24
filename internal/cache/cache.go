// SPDX-License-Identifier: GPL-3.0-or-later
package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Stats is deliberately small so the home screen never needs to rescan disk.
type Stats struct {
	Items   int64     `json:"items"`
	Bytes   int64     `json:"bytes"`
	LastRun time.Time `json:"last_run"`
	Module  string    `json:"module"`
}

func Path(home, bin, module string) string {
	return filepath.Join(home, ".cache", bin, "modules", module+".json")
}

func Save(home, bin string, stats Stats) error {
	if stats.Module == "" {
		return errors.New("cache stats require a module")
	}
	stats.LastRun = time.Now().UTC()
	p := Path(home, bin, stats.Module)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".stats-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

func Load(home, bin, module string) (Stats, error) {
	b, err := os.ReadFile(Path(home, bin, module))
	if err != nil {
		return Stats{}, err
	}
	var stats Stats
	if err := json.Unmarshal(b, &stats); err != nil {
		return Stats{}, err
	}
	return stats, nil
}
