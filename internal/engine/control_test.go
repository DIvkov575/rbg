package engine

import (
	"errors"
	"testing"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
)

// fakeRepo is a canned host.Repo capturing whether Pull ran.
type fakeRepo struct {
	pulled  *string
	pullErr error
	status  core.Sync
}

func (f fakeRepo) Status(dir string) (core.Sync, error) { return f.status, nil }
func (f fakeRepo) Pull(dir string) error {
	if f.pulled != nil {
		*f.pulled = dir
	}
	return f.pullErr
}

// fakeRunner is a canned host.Runner.
type fakeRunner struct {
	res       host.RunResult
	launchErr error
	sendErr   error
	killErr   error
	launched  *string
	sent      *[2]string
	killed    *string
}

func (f fakeRunner) Launch(name, task string) (host.RunResult, error) {
	if f.launched != nil {
		*f.launched = name
	}
	return f.res, f.launchErr
}
func (f fakeRunner) Send(id, task string) error {
	if f.sent != nil {
		*f.sent = [2]string{id, task}
	}
	return f.sendErr
}
func (f fakeRunner) Kill(name string) error {
	if f.killed != nil {
		*f.killed = name
	}
	return f.killErr
}

func runnerFactory(r host.Runner) func(string) host.Runner {
	return func(string) host.Runner { return r }
}

func TestRunRemoteSyncsThenLaunchesAndRecords(t *testing.T) {
	var pulledDir, launchedName string
	rem := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{pulled: &pulledDir},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "job", Session: "S9", Pid: 0}, launched: &launchedName}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{
		Name: "job", Repo: "git@github:me/app", Dir: "/srv/app",
		Task: "do it", Where: core.Remote, State: core.Held, Origin: core.Managed,
	})

	if err := e.Run("job"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pulledDir != "/srv/app" {
		t.Errorf("sync-first: Pull ran on %q, want /srv/app", pulledDir)
	}
	if launchedName != "job" {
		t.Errorf("launched %q, want job", launchedName)
	}
	rec, _ := e.store.Get("job")
	if rec.State != core.Running {
		t.Errorf("State = %q, want running", rec.State)
	}
	if rec.Session != "S9" {
		t.Errorf("Session = %q, want S9", rec.Session)
	}
	if rec.RunAt == "" {
		t.Errorf("RunAt should be set after Run")
	}
}

func TestRunLocalRecordsPid(t *testing.T) {
	loc := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "l", Session: "LS", Pid: 5555}}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "l", Dir: "/home/me/app", Task: "t", Where: core.Local, State: core.Held, Origin: core.Managed})

	if err := e.Run("l"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec, _ := e.store.Get("l")
	if rec.Pid != 5555 {
		t.Errorf("Pid = %d, want 5555", rec.Pid)
	}
}

func TestRunAbortsOnSyncFailure(t *testing.T) {
	var launched string
	rem := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{pullErr: errors.New("would need merge")},
		newRunner: runnerFactory(fakeRunner{launched: &launched}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "j", Repo: "r", Dir: "/srv/j", Task: "t", Where: core.Remote, State: core.Held, Origin: core.Managed})

	if err := e.Run("j"); err == nil {
		t.Errorf("Run should abort when sync-first Pull fails")
	}
	if launched != "" {
		t.Errorf("must NOT launch when sync failed, but launched %q", launched)
	}
	rec, _ := e.store.Get("j")
	if rec.State != core.Held {
		t.Errorf("State = %q, want held (unchanged on sync failure)", rec.State)
	}
}

func TestRunSkipsSyncWhenNoRepo(t *testing.T) {
	var pulled string
	loc := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{pulled: &pulled},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "n", Session: "S"}}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "n", Repo: "", Dir: "/tmp", Task: "t", Where: core.Local, State: core.Held, Origin: core.Managed})

	if err := e.Run("n"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pulled != "" {
		t.Errorf("no repo → no Pull, but pulled %q", pulled)
	}
}

func TestRunUnknownOrUnmanagedErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	if err := e.Run("ghost"); err == nil {
		t.Errorf("running an unknown agent should error")
	}
}
