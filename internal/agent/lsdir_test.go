package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLsdir_ListsSubdirsAsJSON(t *testing.T) {
	base := t.TempDir()
	for _, sub := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(base, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(base, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "afile.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &Agent{}
	var out bytes.Buffer
	if code := a.Lsdir(&out, base); code != 0 {
		t.Fatalf("Lsdir code=%d out=%s", code, out.String())
	}
	var listing DirListing
	if err := json.Unmarshal(out.Bytes(), &listing); err != nil {
		t.Fatalf("bad json: %v (%s)", err, out.String())
	}
	if listing.Dir != base {
		t.Fatalf("dir = %q, want %q", listing.Dir, base)
	}
	if listing.Parent != filepath.Dir(base) {
		t.Fatalf("parent = %q, want %q", listing.Parent, filepath.Dir(base))
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("want 2 entries (no file, no dotdir), got %+v", listing.Entries)
	}
	if listing.Entries[0].Name != "alpha" || listing.Entries[1].Name != "beta" {
		t.Fatalf("entries not sorted/filtered: %+v", listing.Entries)
	}
	if listing.Entries[0].Path != filepath.Join(base, "alpha") {
		t.Fatalf("entry path = %q", listing.Entries[0].Path)
	}
}

func TestLsdir_BadDirErrors(t *testing.T) {
	a := &Agent{}
	var out bytes.Buffer
	code := a.Lsdir(&out, filepath.Join(t.TempDir(), "does-not-exist"))
	if code != 1 {
		t.Fatalf("want code 1 for bad dir, got %d (%s)", code, out.String())
	}
	var obj map[string]any
	if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
		t.Fatalf("expected JSON error object, got %q (%v)", out.String(), err)
	}
	if _, ok := obj["error"]; !ok {
		t.Fatalf("expected error field, got %+v", obj)
	}
}

// DirListing/DirEntry mirror the client-side types; the agent emits this shape.
type DirListing struct {
	Dir     string     `json:"dir"`
	Parent  string     `json:"parent"`
	Entries []DirEntry `json:"entries"`
}
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
