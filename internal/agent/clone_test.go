package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/run"
)

func TestClone_ReusesExistingClone(t *testing.T) {
	home := t.TempDir()
	a := &Agent{Runner: &run.Recording{}, ClaudeHome: home, ReposRoot: filepath.Join(home, "rbg-repos")}
	// pre-create the clone dir so it should be REUSED (no git call)
	dest := filepath.Join(home, "rbg-repos", "my-svc")
	os.MkdirAll(filepath.Join(dest, ".git"), 0o755)
	var out bytes.Buffer
	if code := a.Clone(&out, "https://github.com/me/my-svc.git"); code != 0 {
		t.Fatalf("Clone code=%d out=%s", code, out.String())
	}
	var resp map[string]string
	json.Unmarshal(out.Bytes(), &resp)
	if resp["dir"] != dest {
		t.Fatalf("dir = %q, want %q", resp["dir"], dest)
	}
}

func TestClone_RunsGitWhenAbsent(t *testing.T) {
	home := t.TempDir()
	rec := &run.Recording{Default: run.Result{Code: 0}}
	a := &Agent{Runner: rec, ClaudeHome: home, ReposRoot: filepath.Join(home, "rbg-repos")}
	var out bytes.Buffer
	if code := a.Clone(&out, "https://github.com/me/web-app.git"); code != 0 {
		t.Fatalf("Clone code=%d", code)
	}
	// it should have invoked git clone with the URL and the dest dir
	joined := ""
	for _, c := range rec.Calls {
		joined += c.Name + " " + stringsJoin(c.Args) + "\n"
	}
	if !contains(joined, "git") || !contains(joined, "clone") || !contains(joined, "web-app") {
		t.Fatalf("expected git clone call, got: %s", joined)
	}
}

func TestRepoName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/me/my-svc.git": "my-svc",
		"https://github.com/me/my-svc":     "my-svc",
		"git@github.com:me/web-app.git":    "web-app",
		"my-svc":                           "my-svc",
	}
	for in, want := range cases {
		if got := RepoName(in); got != want {
			t.Errorf("RepoName(%q) = %q, want %q", in, got, want)
		}
	}
}

// tiny local helpers to avoid importing strings in this test file twice
func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
func stringsJoin(a []string) string {
	out := ""
	for i, s := range a {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}
