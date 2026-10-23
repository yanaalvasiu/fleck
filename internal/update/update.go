// SPDX-License-Identifier: GPL-3.0-or-later
package update

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url"`
	CheckedAt time.Time `json:"checked_at"`
	Available bool      `json:"available"`
}
type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	HTML       string `json:"html_url"`
}

func Compare(a, b string) int {
	av := nums(a)
	bv := nums(b)
	for len(av) < 3 {
		av = append(av, 0)
	}
	for len(bv) < 3 {
		bv = append(bv, 0)
	}
	for i := 0; i < 3; i++ {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}
func nums(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	var out []int
	for _, x := range strings.Split(s, ".") {
		n := 0
		for _, r := range x {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}
func Fetch(url, current string, client *http.Client) (Result, error) {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	r, err := client.Get(url)
	if err != nil {
		return Result{}, err
	}
	defer r.Body.Close()
	var rel release
	if err := json.NewDecoder(r.Body).Decode(&rel); err != nil {
		return Result{}, err
	}
	if rel.Draft || rel.Prerelease {
		return Result{Current: current, CheckedAt: time.Now()}, nil
	}
	return Result{Current: current, Latest: strings.TrimPrefix(rel.Tag, "v"), URL: rel.HTML, CheckedAt: time.Now(), Available: Compare(current, rel.Tag) < 0}, nil
}
func LoadCache(home, bin string) (Result, error) {
	b, err := os.ReadFile(filepath.Join(home, ".cache", bin, "update.json"))
	if err != nil {
		return Result{}, err
	}
	var r Result
	err = json.Unmarshal(b, &r)
	return r, err
}
func SaveCache(home, bin string, r Result) error {
	p := filepath.Join(home, ".cache", bin, "update.json")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0600)
}
