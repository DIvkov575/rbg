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
