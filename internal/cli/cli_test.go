package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/core"
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
	var out bytes.Buffer
	code := Dispatch([]string{"frob"}, &fakeOps{}, &out)
	if code == 0 {
		t.Errorf("unknown verb should return non-zero, got 0")
	}
	if !strings.Contains(out.String(), "frob") && !strings.Contains(out.String(), "unknown") {
		t.Errorf("unknown-verb output should mention the verb or 'unknown': %q", out.String())
	}
}

func TestDispatchNoArgs(t *testing.T) {
	var out bytes.Buffer
	code := Dispatch(nil, &fakeOps{}, &out)
	if code == 0 {
		t.Errorf("no args should return non-zero (usage), got 0")
	}
}

func TestDispatchLsRendersAndSucceeds(t *testing.T) {
	var out bytes.Buffer
	ops := &fakeOps{agents: []core.Agent{
		{Name: "a", Where: core.Remote, State: core.Running, Origin: core.Managed},
	}}
	code := Dispatch([]string{"ls"}, ops, &out)
	if code != 0 {
		t.Errorf("ls should succeed, got code %d", code)
	}
	if !strings.Contains(out.String(), "a") {
		t.Errorf("ls output missing the agent: %q", out.String())
	}
}

func TestDispatchLsDegradedStillRendersButWarns(t *testing.T) {
	var out bytes.Buffer
	ops := &fakeOps{
		agents:  []core.Agent{{Name: "a", Where: core.Local, State: core.Held, Origin: core.Managed}},
		listErr: errTest,
	}
	code := Dispatch([]string{"ls"}, ops, &out)
	if !strings.Contains(out.String(), "a") {
		t.Errorf("degraded ls should still render the agent: %q", out.String())
	}
	if code == 0 {
		t.Errorf("degraded ls should signal non-zero")
	}
}

var errTest = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }
