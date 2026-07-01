package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
)

// fakeOps is a canned Ops for dispatch tests, recording calls.
type fakeOps struct {
	agents    []core.Agent
	listErr   error
	created   *core.Agent
	createErr error
	ran       *string
	runErr    error
	sent      *[2]string
	sendErr   error
	readData  []byte
	readErr   error
	killed    *string
	killErr   error
	adopted   *string
	adoptErr  error
}

func (f *fakeOps) List() ([]core.Agent, error) { return f.agents, f.listErr }
func (f *fakeOps) Create(a core.Agent) (core.Agent, error) {
	f.created = &a
	return a, f.createErr
}
func (f *fakeOps) Run(name string) error            { f.ran = &name; return f.runErr }
func (f *fakeOps) Send(name, task string) error     { f.sent = &[2]string{name, task}; return f.sendErr }
func (f *fakeOps) Read(name string) ([]byte, error) { return f.readData, f.readErr }
func (f *fakeOps) Kill(name string) error           { f.killed = &name; return f.killErr }
func (f *fakeOps) Adopt(name string) error          { f.adopted = &name; return f.adoptErr }

func TestDispatchUnknownVerb(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch([]string{"frob"}, &fakeOps{}, &out, &errOut)
	if code == 0 {
		t.Errorf("unknown verb should return non-zero, got 0")
	}
	if !strings.Contains(errOut.String(), "frob") && !strings.Contains(errOut.String(), "unknown") {
		t.Errorf("unknown-verb output should mention the verb or 'unknown': %q", errOut.String())
	}
}

func TestDispatchNoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Dispatch(nil, &fakeOps{}, &out, &errOut)
	if code == 0 {
		t.Errorf("no args should return non-zero (usage), got 0")
	}
	if errOut.Len() == 0 {
		t.Errorf("no args should print usage to errOut")
	}
}

func TestDispatchLsRendersAndSucceeds(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{agents: []core.Agent{
		{Name: "a", Where: core.Remote, State: core.Running, Origin: core.Managed},
	}}
	code := Dispatch([]string{"ls"}, ops, &out, &errOut)
	if code != 0 {
		t.Errorf("ls should succeed, got code %d", code)
	}
	if !strings.Contains(out.String(), "a") {
		t.Errorf("ls output missing the agent: %q", out.String())
	}
}

func TestDispatchLsDegradedStillRendersButWarns(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{
		agents:  []core.Agent{{Name: "a", Where: core.Local, State: core.Held, Origin: core.Managed}},
		listErr: errTest,
	}
	code := Dispatch([]string{"ls"}, ops, &out, &errOut)
	if !strings.Contains(out.String(), "a") {
		t.Errorf("degraded ls should still render the agent: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "warning") {
		t.Errorf("degraded ls should warn to errOut: %q", errOut.String())
	}
	if code == 0 {
		t.Errorf("degraded ls should signal non-zero")
	}
}

var errTest = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }

func TestDispatchCreate(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{}
	code := Dispatch([]string{"create", "later", "git@github:me/app", "refactor parser"}, ops, &out, &errOut)
	if code != 0 {
		t.Fatalf("create should succeed, got %d (%s)", code, errOut.String())
	}
	if ops.created == nil {
		t.Fatalf("Create was not called")
	}
	if ops.created.Name != "later" || ops.created.Repo != "git@github:me/app" || ops.created.Task != "refactor parser" {
		t.Errorf("create built wrong spec: %+v", *ops.created)
	}
}

func TestDispatchCreateWrongArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Dispatch([]string{"create", "onlyname"}, &fakeOps{}, &out, &errOut); code != 2 {
		t.Errorf("create with too few args should be usage error (2), got %d", code)
	}
	if errOut.Len() == 0 {
		t.Errorf("usage error should print to errOut")
	}
}

func TestDispatchRun(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{}
	if code := Dispatch([]string{"run", "later"}, ops, &out, &errOut); code != 0 {
		t.Fatalf("run should succeed, got %d", code)
	}
	if ops.ran == nil || *ops.ran != "later" {
		t.Errorf("Run called with %v, want later", ops.ran)
	}
}

func TestDispatchRunEngineErrorIsExit1(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{runErr: errTest}
	if code := Dispatch([]string{"run", "later"}, ops, &out, &errOut); code != 1 {
		t.Errorf("run with engine error should exit 1, got %d", code)
	}
}

func TestDispatchAdoptAndKill(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{}
	if code := Dispatch([]string{"adopt", "wild"}, ops, &out, &errOut); code != 0 {
		t.Fatalf("adopt should succeed")
	}
	if ops.adopted == nil || *ops.adopted != "wild" {
		t.Errorf("Adopt called with %v", ops.adopted)
	}
	if code := Dispatch([]string{"kill", "job"}, ops, &out, &errOut); code != 0 {
		t.Fatalf("kill should succeed")
	}
	if ops.killed == nil || *ops.killed != "job" {
		t.Errorf("Kill called with %v", ops.killed)
	}
}

func TestDispatchNameVerbsRequireName(t *testing.T) {
	for _, v := range []string{"run", "adopt", "kill"} {
		var out, errOut bytes.Buffer
		if code := Dispatch([]string{v}, &fakeOps{}, &out, &errOut); code != 2 {
			t.Errorf("%s without a name should be usage error (2), got %d", v, code)
		}
	}
}

func TestDispatchSend(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{}
	if code := Dispatch([]string{"send", "job", "next step"}, ops, &out, &errOut); code != 0 {
		t.Fatalf("send should succeed, got %d", code)
	}
	if ops.sent == nil || ops.sent[0] != "job" || ops.sent[1] != "next step" {
		t.Errorf("Send called with %v", ops.sent)
	}
}

func TestDispatchSendBusyIsClear(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{sendErr: host.ErrBusy}
	code := Dispatch([]string{"send", "job", "x"}, ops, &out, &errOut)
	if code == 0 {
		t.Errorf("busy send should be non-zero")
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "busy") {
		t.Errorf("busy message should mention busy: %q", errOut.String())
	}
}

func TestDispatchSendWrongArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Dispatch([]string{"send", "job"}, &fakeOps{}, &out, &errOut); code != 2 {
		t.Errorf("send needs <name> <task>, want usage error 2, got %d", code)
	}
}

func TestDispatchRead(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{readData: []byte(`{"type":"user","message":{"role":"user","content":"hello there"}}` + "\n")}
	if code := Dispatch([]string{"read", "job"}, ops, &out, &errOut); code != 0 {
		t.Fatalf("read should succeed, got %d", code)
	}
	if !strings.Contains(out.String(), "hello there") {
		t.Errorf("read output should contain the rendered transcript text: %q", out.String())
	}
}

func TestDispatchReadErrorIsExit1(t *testing.T) {
	var out, errOut bytes.Buffer
	ops := &fakeOps{readErr: errTest}
	if code := Dispatch([]string{"read", "job"}, ops, &out, &errOut); code != 1 {
		t.Errorf("read engine error should exit 1, got %d", code)
	}
}
