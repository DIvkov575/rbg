package host

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
)

func TestLocalRepoStatusAligned(t *testing.T) {
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},        // clean
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0}, // has upstream
		"rev-list":  {Stdout: []byte("0\t0\n"), Code: 0},   // 0 behind, 0 ahead
	}}
	got, err := LocalRepo{R: r}.Status("/repo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != core.Aligned {
		t.Errorf("Status = %q, want aligned", got)
	}
}

func TestLocalRepoStatusBehind(t *testing.T) {
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0},
		"rev-list":  {Stdout: []byte("4\t0\n"), Code: 0}, // 4 behind
	}}
	got, _ := LocalRepo{R: r}.Status("/repo")
	if got != core.Behind {
		t.Errorf("Status = %q, want behind", got)
	}
}

func TestLocalRepoStatusDirty(t *testing.T) {
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(" M file.go\n"), Code: 0}, // dirty
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0},
		"rev-list":  {Stdout: []byte("0\t0\n"), Code: 0},
	}}
	got, _ := LocalRepo{R: r}.Status("/repo")
	if got != core.Dirty {
		t.Errorf("Status = %q, want dirty", got)
	}
}

func TestLocalRepoStatusNoUpstream(t *testing.T) {
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},
		"rev-parse": {Stdout: []byte("fatal: no upstream\n"), Code: 128}, // no upstream
	}}
	got, err := LocalRepo{R: r}.Status("/repo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != core.SyncUnknown {
		t.Errorf("Status = %q, want unknown", got)
	}
}

func TestLocalRepoStatusUsesGitDashCInDir(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte(""), Code: 0}}
	_, _ = LocalRepo{R: r}.Status("/my/repo")
	if len(r.Calls) == 0 {
		t.Fatal("no git calls made")
	}
	for _, c := range r.Calls {
		if c.Name != "git" {
			t.Errorf("ran %q, want git", c.Name)
		}
		j := joined(c.Args)
		if !contains(j, "-C") || !contains(j, "/my/repo") {
			t.Errorf("git call not scoped to dir: %v", c.Args)
		}
	}
}

func TestLocalRepoPull(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 0}}
	if err := (LocalRepo{R: r}).Pull("/repo"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "pull") || !contains(j, "--ff-only") || !contains(j, "/repo") {
		t.Errorf("pull args wrong: %v", r.Calls[0].Args)
	}
}

func TestLocalRepoPullFailsOnNonZero(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte("conflict"), Code: 1}}
	if err := (LocalRepo{R: r}).Pull("/repo"); err == nil {
		t.Errorf("expected error on non-zero pull exit")
	}
}
