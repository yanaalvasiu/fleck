// SPDX-License-Identifier: GPL-3.0-or-later
package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	if Compare("1.2.0", "1.10.0") >= 0 || Compare("v2.0.0", "1.9.0") <= 0 || Compare("1.0", "1.0.0") != 0 {
		t.Fatal("semver compare")
	}
}
func TestFetchAndCache(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v2.0.0","html_url":"https://example.test/release"}`))
	}))
	defer s.Close()
	got, err := Fetch(s.URL, "1.0.0", &http.Client{Timeout: time.Second})
	if err != nil || !got.Available {
		t.Fatalf("fetch: %#v %v", got, err)
	}
	h := t.TempDir()
	if err := SaveCache(h, "bin", got); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCache(h, "bin"); err != nil {
		t.Fatal(err)
	}
}
