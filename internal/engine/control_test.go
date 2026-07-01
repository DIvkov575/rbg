package engine

import (
	"errors"
	"strings"
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

func TestRunPinsLaunchDirWhenRecordDirEmpty(t *testing.T) {
	// A repo-less agent has Dir="" at Create; Run must persist the dir the runner
	// actually launched in, so a later Send resumes in the same place.
	loc := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "n", Session: "S", Pid: 1, Dir: "/real/cwd"}}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "n", Dir: "", Task: "t", Where: core.Local, State: core.Held, Origin: core.Managed})

	if err := e.Run("n"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec, _ := e.store.Get("n")
	if rec.Dir != "/real/cwd" {
		t.Errorf("Dir = %q, want pinned /real/cwd", rec.Dir)
	}
}

func TestRunKeepsExplicitDirOverLaunchDir(t *testing.T) {
	// If the record already has a Dir (repo-backed), Run must not overwrite it
	// with the runner's report.
	loc := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "n", Session: "S", Dir: "/other"}}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "n", Dir: "/explicit", Task: "t", Where: core.Local, State: core.Held, Origin: core.Managed})

	if err := e.Run("n"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	rec, _ := e.store.Get("n")
	if rec.Dir != "/explicit" {
		t.Errorf("Dir = %q, want /explicit (unchanged)", rec.Dir)
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

func TestRunRefusesToReRunRunningAgent(t *testing.T) {
	// Re-running would overwrite the live Session/Pid and orphan the child.
	var launched string
	loc := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{},
		newRunner: runnerFactory(fakeRunner{launched: &launched}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "busy", Session: "OLD", Pid: 999, Task: "t", Where: core.Local, State: core.Running, Origin: core.Managed})

	if err := e.Run("busy"); err == nil {
		t.Errorf("Run on an already-running agent should error")
	}
	if launched != "" {
		t.Errorf("must NOT launch a second child, but launched %q", launched)
	}
	rec, _ := e.store.Get("busy")
	if rec.Session != "OLD" || rec.Pid != 999 {
		t.Errorf("live identity was overwritten: %+v", rec)
	}
}

func TestRunRejectsRepoWithoutDir(t *testing.T) {
	var pulled string
	loc := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{pulled: &pulled},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "x", Session: "S"}}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "x", Repo: "app", Dir: "", Task: "t", Where: core.Local, State: core.Held, Origin: core.Managed})

	if err := e.Run("x"); err == nil {
		t.Errorf("Run should reject a repo-backed agent with an empty dir")
	}
	if pulled != "" {
		t.Errorf("must not pull with an empty dir, pulled %q", pulled)
	}
}

func TestRunAdoptsResolvedNameFromLaunch(t *testing.T) {
	// The desktop rbg-agent deduped the name (job → job-2); Run must persist the
	// resolved name and re-key the record so later Send/Kill target the right id.
	rem := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "job-2", Session: "S9"}}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job", Task: "t", Where: core.Remote, State: core.Held, Origin: core.Managed})

	if err := e.Run("job"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := e.store.Get("job"); ok {
		t.Errorf("old name %q should have been removed after dedup", "job")
	}
	rec, ok := e.store.Get("job-2")
	if !ok {
		t.Fatalf("record not re-keyed to resolved name job-2")
	}
	if rec.Session != "S9" || rec.State != core.Running {
		t.Errorf("re-keyed record wrong: %+v", rec)
	}
}

func TestRunNameCollisionMovesOccupantAsideAndAdoptsResolvedName(t *testing.T) {
	// The desktop deduped to "job-2", which a DIFFERENT rbg record (a local agent)
	// already holds. The launched agent MUST take "job-2" (remote Send/Kill key by
	// name), and the occupant is moved to a fresh key so nothing is lost or
	// clobbered.
	rem := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{},
		newRunner: runnerFactory(fakeRunner{res: host.RunResult{Name: "job-2", Session: "NEW"}}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job-2", Session: "EXISTING", Pid: 777, Where: core.Local, State: core.Running, Origin: core.Managed, Task: "other"})
	e.store.Add(core.Agent{Name: "job", Task: "t", Where: core.Remote, State: core.Held, Origin: core.Managed})

	if err := e.Run("job"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The launched remote agent took the resolved name with its new session.
	launched, ok := e.store.Get("job-2")
	if !ok || launched.Session != "NEW" || launched.Where != core.Remote {
		t.Errorf("launched agent should own 'job-2' with the new session: %+v ok=%v", launched, ok)
	}
	// The original 'job' key is gone (re-keyed to job-2).
	if _, ok := e.store.Get("job"); ok {
		t.Errorf("original 'job' key should be removed after adopting resolved name")
	}
	// The displaced occupant survives under a fresh key with its identity intact.
	moved, ok := e.store.Get("job-2-2")
	if !ok || moved.Session != "EXISTING" || moved.Pid != 777 {
		t.Errorf("displaced occupant should survive at a fresh key with identity intact, got %+v ok=%v", moved, ok)
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

func TestSendRemoteUsesName(t *testing.T) {
	var sent [2]string
	rem := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "S1", Name: "job", Cwd: "/srv", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{sent: &sent}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job", Session: "S1", Where: core.Remote, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Send("job", "next step"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent[0] != "job" || sent[1] != "next step" {
		t.Errorf("remote Send got %v, want [job, next step] (by name)", sent)
	}
}

func TestSendLocalUsesSession(t *testing.T) {
	var sent [2]string
	loc := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "LS", Name: "loc", Cwd: "/x", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{sent: &sent}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "loc", Session: "LS", Where: core.Local, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Send("loc", "more"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent[0] != "LS" || sent[1] != "more" {
		t.Errorf("local Send got %v, want [LS, more] (by session)", sent)
	}
}

func TestSendPropagatesBusy(t *testing.T) {
	rem := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "S1", Name: "job", Cwd: "/srv", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{sendErr: host.ErrBusy}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job", Session: "S1", Where: core.Remote, State: core.Running, Origin: core.Managed, Task: "t"})

	err := e.Send("job", "x")
	if err != host.ErrBusy {
		t.Errorf("Send err = %v, want host.ErrBusy", err)
	}
}

func TestSendToNeverRunErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	e.store.Add(core.Agent{Name: "held", Task: "t", State: core.Held, Origin: core.Managed})
	if err := e.Send("held", "x"); err == nil {
		t.Errorf("sending to a never-run agent should error")
	}
}

func TestKillRemoteCallsRunnerKill(t *testing.T) {
	var killed string
	rem := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "S1", Name: "job", Cwd: "/srv", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{killed: &killed}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job", Session: "S1", Where: core.Remote, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Kill("job"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if killed != "job" {
		t.Errorf("remote Kill got %q, want job", killed)
	}
	rec, _ := e.store.Get("job")
	if rec.State != core.Done {
		t.Errorf("State = %q, want done after kill", rec.State)
	}
}

func TestKillLocalUsesPid(t *testing.T) {
	var killedPid int
	loc := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "LS", Name: "loc", Cwd: "/x", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.killLocal = func(pid int) error { killedPid = pid; return nil }
	e.store.Add(core.Agent{Name: "loc", Session: "LS", Pid: 4242, Where: core.Local, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Kill("loc"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if killedPid != 4242 {
		t.Errorf("local Kill signalled pid %d, want 4242", killedPid)
	}
	rec, _ := e.store.Get("loc")
	if rec.State != core.Done {
		t.Errorf("State = %q, want done after kill", rec.State)
	}
}

func TestKillLocalWithoutPidErrors(t *testing.T) {
	loc := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "LS", Name: "loc", Cwd: "/x", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.killLocal = func(pid int) error { t.Fatalf("must not kill with no pid"); return nil }
	e.store.Add(core.Agent{Name: "loc", Session: "LS", Pid: 0, Where: core.Local, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Kill("loc"); err == nil {
		t.Errorf("local Kill with no recorded pid should error")
	}
}

func TestKillForeignLocalAgentGivesClearError(t *testing.T) {
	// A foreign local agent has no tracked pid (claude exposes none), so it can't
	// be killed via rbg — the error must say why rather than look like a bug.
	loc := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "F1", Name: "wild", Cwd: "/x", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{}),
	}
	e := newTestEngine(t, loc, machine{Source: fakeSource{}})
	e.killLocal = func(pid int) error { t.Fatalf("must not attempt to kill without a pid"); return nil }

	err := e.Kill("wild")
	if err == nil {
		t.Fatalf("killing a foreign local agent should error")
	}
	if !strings.Contains(err.Error(), "foreign") {
		t.Errorf("error should explain the foreign-agent limitation, got: %v", err)
	}
}

func TestKillUnknownErrors(t *testing.T) {
	e := newTestEngine(t, machine{Source: fakeSource{}}, machine{Source: fakeSource{}})
	if err := e.Kill("ghost"); err == nil {
		t.Errorf("killing an unknown agent should error")
	}
}

func TestRunLeavesStateUnchangedOnLaunchFailure(t *testing.T) {
	// Launch (not sync) fails: the record must stay Held with no Session/Pid/RunAt
	// recorded — nothing partial escapes.
	rem := machine{
		Source:    fakeSource{},
		Repo:      fakeRepo{},
		newRunner: runnerFactory(fakeRunner{launchErr: errors.New("spawn refused")}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "j", Repo: "r", Dir: "/srv/j", Task: "t", Where: core.Remote, State: core.Held, Origin: core.Managed})

	if err := e.Run("j"); err == nil {
		t.Fatalf("Run should error when Launch fails")
	}
	rec, _ := e.store.Get("j")
	if rec.State != core.Held {
		t.Errorf("State = %q, want held (unchanged on launch failure)", rec.State)
	}
	if rec.Session != "" || rec.RunAt != "" {
		t.Errorf("no live identity should be recorded on launch failure: %+v", rec)
	}
}

func TestKillRemoteFailureLeavesStateUnchanged(t *testing.T) {
	// RemoteRunner.Kill fails: the record must NOT be marked Done (the agent may
	// still be running).
	rem := machine{
		Source:    fakeSource{live: []core.Live{{SessionID: "S1", Name: "job", Cwd: "/srv", State: "working"}}},
		newRunner: runnerFactory(fakeRunner{killErr: errors.New("ssh failed")}),
	}
	e := newTestEngine(t, machine{Source: fakeSource{}}, rem)
	e.store.Add(core.Agent{Name: "job", Session: "S1", Where: core.Remote, State: core.Running, Origin: core.Managed, Task: "t"})

	if err := e.Kill("job"); err == nil {
		t.Fatalf("Kill should error when the remote kill fails")
	}
	rec, _ := e.store.Get("job")
	if rec.State != core.Running {
		t.Errorf("State = %q, want running (unchanged when kill failed)", rec.State)
	}
}
