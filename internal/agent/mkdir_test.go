package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMkdir_CreatesDir(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "newdir")

	a := &Agent{}
	var out bytes.Buffer
	if code := a.Mkdir(&out, target); code != 0 {
		t.Fatalf("Mkdir code=%d out=%s", code, out.String())
	}
	if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
		t.Fatalf("dir not created: err=%v", err)
	}
	var obj map[string]string
	if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
		t.Fatalf("bad json: %v (%s)", err, out.String())
	}
	if obj["dir"] != target {
		t.Fatalf("dir field = %q, want %q", obj["dir"], target)
	}
}

func TestMkdir_EmptyErrors(t *testing.T) {
	a := &Agent{}
	var out bytes.Buffer
	if code := a.Mkdir(&out, ""); code != 1 {
		t.Fatalf("want code 1 for empty dir, got %d (%s)", code, out.String())
	}
	var obj map[string]any
	if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
		t.Fatalf("expected JSON error object, got %q (%v)", out.String(), err)
	}
	if _, ok := obj["error"]; !ok {
		t.Fatalf("expected error field, got %+v", obj)
	}
}
