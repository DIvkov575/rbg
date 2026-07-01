package host

import (
	"testing"

	"github.com/divkov575/rbg/internal/config"
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

func TestLocalRepoStatusAhead(t *testing.T) {
	// Pins the left/right mapping: right column = ahead. A swapped mapping would
	// otherwise pass the aligned(0\t0) and behind(4\t0) cases undetected.
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0},
		"rev-list":  {Stdout: []byte("0\t3\n"), Code: 0}, // 0 behind, 3 ahead
	}}
	got, err := LocalRepo{R: r}.Status("/repo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != core.Ahead {
		t.Errorf("Status = %q, want ahead", got)
	}
}

func TestLocalRepoStatusRevListMalformedErrors(t *testing.T) {
	// Upstream exists but rev-list output is unparseable → error (not a silent
	// misclassification).
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0},
		"rev-list":  {Stdout: []byte("garbage-not-two-ints\n"), Code: 0},
	}}
	if _, err := (LocalRepo{R: r}).Status("/repo"); err == nil {
		t.Errorf("expected error on unparseable rev-list output")
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

func TestRemoteRepoStatusRunsGitOverSSH(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	r := &run.Recording{BySubstring: map[string]run.Result{
		"status":    {Stdout: []byte(""), Code: 0},
		"rev-parse": {Stdout: []byte("origin/main\n"), Code: 0},
		"rev-list":  {Stdout: []byte("0\t1\n"), Code: 0}, // 1 ahead
	}}
	got, err := RemoteRepo{C: cfg, R: r}.Status("/srv/repo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != core.Ahead {
		t.Errorf("Status = %q, want ahead", got)
	}
	// every call is ssh, and the git command + dir + host must appear.
	for _, c := range r.Calls {
		if c.Name != "ssh" {
			t.Errorf("ran %q, want ssh", c.Name)
		}
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "desktop") || !contains(j, "git") || !contains(j, "/srv/repo") {
		t.Errorf("ssh git call missing host/git/dir: %v", r.Calls[0].Args)
	}
}

func TestRemoteRepoPull(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 0}}
	if err := (RemoteRepo{C: cfg, R: r}).Pull("/srv/repo"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if r.Calls[0].Name != "ssh" {
		t.Errorf("ran %q, want ssh", r.Calls[0].Name)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "pull") || !contains(j, "--ff-only") || !contains(j, "/srv/repo") {
		t.Errorf("remote pull args wrong: %v", r.Calls[0].Args)
	}
}

func TestRemoteRepoConfigsConnectTimeout(t *testing.T) {
	// A down host must surface as an error via ssh's own non-zero exit, not hang.
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte("ssh: connect timeout"), Code: 255}}
	if _, err := (RemoteRepo{C: cfg, R: r}).Status("/srv/repo"); err == nil {
		t.Errorf("expected error when ssh fails (exit 255)")
	}
}
