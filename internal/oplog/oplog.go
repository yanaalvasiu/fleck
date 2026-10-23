// SPDX-License-Identifier: GPL-3.0-or-later
package oplog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Entry struct {
	Time                         time.Time `json:"time"`
	Action, Module, Path, Result string
}

func Append(home, bin string, entry Entry) error {
	if os.Getenv(bin+"_NO_OPLOG") == "1" {
		return nil
	}
	dir := filepath.Join(home, "Library", "Logs", bin)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "operations.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func Read(home, bin string) ([]Entry, error) {
	f, err := os.Open(filepath.Join(home, "Library", "Logs", bin, "operations.log"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	s := bufio.NewScanner(f)
	for s.Scan() {
		var e Entry
		if json.Unmarshal(s.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, s.Err()
}
