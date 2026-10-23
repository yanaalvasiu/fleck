// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	UpdateCheck bool
	Colors      bool
	ScanPaths   []string
	Whitelist   []string
}

func Default() Config              { return Config{UpdateCheck: true, Colors: true} }
func Path(home, bin string) string { return filepath.Join(home, ".config", bin, "config.toml") }
func Load(home, bin string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(Path(home, bin))
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "update_check") {
			c.UpdateCheck = !strings.Contains(line, "false")
		}
		if strings.HasPrefix(line, "colors") {
			c.Colors = !strings.Contains(line, "false")
		}
		if strings.HasPrefix(line, "scan_path") || strings.HasPrefix(line, "whitelist") {
			if i := strings.Index(line, "="); i >= 0 {
				v := strings.Trim(strings.TrimSpace(line[i+1:]), "\"[]")
				if v != "" {
					if strings.HasPrefix(line, "scan") {
						c.ScanPaths = append(c.ScanPaths, v)
					} else {
						c.Whitelist = append(c.Whitelist, v)
					}
				}
			}
		}
	}
	return c, nil
}
func Save(home, bin string, c Config) error {
	p := Path(home, bin)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("update_check = ")
	if c.UpdateCheck {
		b.WriteString("true\n")
	} else {
		b.WriteString("false\n")
	}
	b.WriteString("colors = ")
	if c.Colors {
		b.WriteString("true\n")
	} else {
		b.WriteString("false\n")
	}
	for _, x := range c.ScanPaths {
		b.WriteString("scan_path = \"" + x + "\"\n")
	}
	for _, x := range c.Whitelist {
		b.WriteString("whitelist = \"" + x + "\"\n")
	}
	return os.WriteFile(p, []byte(b.String()), 0600)
}
