package host

import (
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)

func joined(args []string) string {
	s := ""
	for _, a := range args {
		s += a + " "
	}
	return s
}

func TestRemoteRunnerLaunchParsesSession(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	r := &run.Recording{BySubstring: map[string]run.Result{
		"launch": {Stdout: []byte(`{"id":"fix-bug","claudeSessionId":"sid-42"}`), Code: 0},
	}}
	res, err := RemoteRunner{C: cfg, R: r}.Launch("fix-bug", "fix the bug")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if res.Name != "fix-bug" || res.Session != "sid-42" {
		t.Errorf("RunResult = %+v, want {fix-bug sid-42}", res)
	}
	// It must go over ssh, invoking the rbg-agent launch verb with the task.
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("expected one ssh call, got %+v", r.Calls)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "launch") || !contains(j, "fix the bug") || !contains(j, "desktop") {
		t.Errorf("ssh args missing launch/task/host: %v", r.Calls[0].Args)
	}
}

func TestRemoteRunnerLaunchUsesDirAsCwd(t *testing.T) {
	// A repo-backed remote agent must launch in the dir engine.Run synced, not
	// the config default — so RemoteRunner.Dir overrides the agent's --cwd.
	cfg := &config.Config{Host: "desktop", CWD: "/default/home", Mux: false}
	r := &run.Recording{BySubstring: map[string]run.Result{
		"launch": {Stdout: []byte(`{"id":"x","claudeSessionId":"s"}`), Code: 0},
	}}
	if _, err := (RemoteRunner{C: cfg, R: r, Dir: "/desk/workplace/app"}).Launch("x", "t"); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "--cwd") || !contains(j, "/desk/workplace/app") {
		t.Errorf("launch should pass --cwd /desk/workplace/app, got %v", r.Calls[0].Args)
	}
	if contains(j, "/default/home") {
		t.Errorf("Dir should override the config CWD, but /default/home leaked: %v", r.Calls[0].Args)
	}
}

func TestRemoteRunnerLaunchNonZeroErrors(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte("boom"), Code: 1}}
	if _, err := (RemoteRunner{C: cfg, R: r}).Launch("n", "t"); err == nil {
		t.Errorf("expected error on non-zero launch exit")
	}
}

func TestRemoteRunnerSendBusyIsError(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 3}} // rbg-agent busy signal
	err := RemoteRunner{C: cfg, R: r}.Send("fix-bug", "more")
	if err == nil {
		t.Fatalf("expected busy error on exit 3")
	}
	if err != ErrBusy {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}

func TestRemoteRunnerSendOK(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 0}}
	if err := (RemoteRunner{C: cfg, R: r}).Send("fix-bug", "more"); err != nil {
		t.Errorf("Send ok: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "send") || !contains(j, "more") {
		t.Errorf("ssh args missing send/task: %v", r.Calls[0].Args)
	}
}

func TestRemoteRunnerKill(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte(`{"ok":"killed","id":"fix-bug"}`), Code: 0}}
	if err := (RemoteRunner{C: cfg, R: r}).Kill("fix-bug"); err != nil {
		t.Errorf("Kill: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "kill") {
		t.Errorf("ssh args missing kill: %v", r.Calls[0].Args)
	}
}

// recordingSpawn captures spawn calls in place of agent.DefaultSpawn.
type recordingSpawn struct {
	calls []spawnCall
	pid   int
	err   error
}
type spawnCall struct {
	name string
	args []string
	dir  string
}

func (rs *recordingSpawn) spawn(name string, args []string, stdoutPath, dir string) (int, error) {
	rs.calls = append(rs.calls, spawnCall{name: name, args: args, dir: dir})
	return rs.pid, rs.err
}

func TestLocalRunnerLaunchGeneratesSessionAndSpawns(t *testing.T) {
	sp := &recordingSpawn{pid: 4321}
	lr := LocalRunner{Spawn: sp.spawn, Dir: "/home/me/app"}
	res, err := lr.Launch("", "fix the bug")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	// Name derived from task via slug; session id generated (36-char uuid).
	if res.Name == "" {
		t.Errorf("empty name; want slug-derived")
	}
	if len(res.Session) != 36 {
		t.Errorf("Session = %q, want a 36-char uuid", res.Session)
	}
	if res.Pid != 4321 {
		t.Errorf("Pid = %d, want 4321 (the spawned pid)", res.Pid)
	}
	if len(sp.calls) != 1 {
		t.Fatalf("made %d spawn calls, want 1", len(sp.calls))
	}
	c := sp.calls[0]
	if c.name != "claude" {
		t.Errorf("spawned %q, want claude", c.name)
	}
	if c.dir != "/home/me/app" {
		t.Errorf("spawn dir = %q, want /home/me/app", c.dir)
	}
	// argv must carry the task, --session-id, and the returned session id.
	j := joined(c.args)
	if !contains(j, "fix the bug") || !contains(j, "--session-id") || !contains(j, res.Session) {
		t.Errorf("spawn args missing task/session-id: %v", c.args)
	}
}

func TestLocalRunnerLaunchHonorsExplicitName(t *testing.T) {
	sp := &recordingSpawn{pid: 1}
	res, err := (LocalRunner{Spawn: sp.spawn}).Launch("my-name", "do it")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if res.Name != "my-name" {
		t.Errorf("Name = %q, want my-name", res.Name)
	}
}

func TestLocalRunnerLaunchSpawnErrorPropagates(t *testing.T) {
	sp := &recordingSpawn{err: errFakeSpawn}
	if _, err := (LocalRunner{Spawn: sp.spawn}).Launch("n", "t"); err == nil {
		t.Errorf("expected spawn error to propagate")
	}
}

func TestLocalRunnerSendSpawnsResume(t *testing.T) {
	sp := &recordingSpawn{pid: 2}
	// Local Send needs the session id to resume; it is passed as the name arg's
	// companion — LocalRunner.Send(session, task) resumes that claude session.
	err := (LocalRunner{Spawn: sp.spawn}).Send("sid-9", "next step")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(sp.calls) != 1 {
		t.Fatalf("made %d spawn calls, want 1", len(sp.calls))
	}
	j := joined(sp.calls[0].args)
	if !contains(j, "--resume") || !contains(j, "sid-9") || !contains(j, "next step") {
		t.Errorf("resume args wrong: %v", sp.calls[0].args)
	}
}

func TestLocalRunnerSendBusyWhenLockHeld(t *testing.T) {
	// While a send to the same session is in flight (its lock held), a second
	// send must return ErrBusy rather than spawning a racing resume — matching
	// the remote exit-3 semantics.
	dir := t.TempDir()
	sp := &recordingSpawn{pid: 1}
	lr := LocalRunner{Spawn: sp.spawn, LockDir: dir}

	// Simulate an in-flight send by holding the session's lock.
	held, ok, err := session.TryLock(lr.lockPath("sid-busy"))
	if err != nil || !ok {
		t.Fatalf("could not pre-acquire lock: ok=%v err=%v", ok, err)
	}
	defer held.Unlock()

	if err := lr.Send("sid-busy", "second"); err != ErrBusy {
		t.Errorf("Send while locked = %v, want ErrBusy", err)
	}
	if len(sp.calls) != 0 {
		t.Errorf("busy Send must not spawn, got %d calls", len(sp.calls))
	}
}

func TestLocalRunnerSendReleasesLockForNextSend(t *testing.T) {
	// After a send completes, its lock is released so a subsequent send succeeds.
	dir := t.TempDir()
	sp := &recordingSpawn{pid: 1}
	lr := LocalRunner{Spawn: sp.spawn, LockDir: dir}

	if err := lr.Send("sid-x", "first"); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	if err := lr.Send("sid-x", "second"); err != nil {
		t.Fatalf("second Send should succeed after lock release: %v", err)
	}
	if len(sp.calls) != 2 {
		t.Errorf("want 2 spawns across two sends, got %d", len(sp.calls))
	}
}

func TestLocalRunnerKillIsNotImplemented(t *testing.T) {
	// LocalRunner cannot kill: local pid tracking lives in the Store/CLI layer.
	// It must return an error (never a silent no-op that looks like success),
	// and it must NOT spawn anything. This pins that deliberate contract.
	sp := &recordingSpawn{}
	err := (LocalRunner{Spawn: sp.spawn}).Kill("some-agent")
	if err == nil {
		t.Errorf("LocalRunner.Kill should return a not-implemented error, got nil")
	}
	if len(sp.calls) != 0 {
		t.Errorf("Kill must not spawn anything, got %d spawn calls", len(sp.calls))
	}
}

var errFakeSpawn = errorString("spawn failed")

type errorString string

func (e errorString) Error() string { return string(e) }
