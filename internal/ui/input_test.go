package ui

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func TestInputTypingAndBackspace(t *testing.T) {
	m := NewModel(nil)
	s := newInputScreen(createMode, "")
	m.push(s)
	for _, r := range "hi" {
		s.Update(m, KeyRune, r)
	}
	if m.Buffer != "hi" {
		t.Errorf("buffer = %q, want hi", m.Buffer)
	}
	s.Update(m, KeyBackspace, 0)
	if m.Buffer != "h" {
		t.Errorf("after backspace, buffer = %q, want h", m.Buffer)
	}
}

func TestInputEscPopsNoAction(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)
	m.Buffer = "abandon me"
	a := s.Update(m, KeyEsc, 0)
	if a.Kind != ActNone {
		t.Errorf("esc should return ActNone, got %v", a.Kind)
	}
	if _, ok := m.Top().(*listScreen); !ok {
		t.Errorf("esc should pop back to the list, top is %T", m.Top())
	}
	if m.Buffer != "" {
		t.Errorf("esc should clear the buffer, got %q", m.Buffer)
	}
}

// typeString feeds each rune of s into the input screen.
func typeString(scr *inputScreen, m *Model, s string) {
	for _, r := range s {
		scr.Update(m, KeyRune, r)
	}
}

func TestInputCreateEnterReturnsSpec(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)

	// stage 0: name → advances, no action.
	typeString(s, m, "agent-x")
	if a := s.Update(m, KeyEnter, 0); a.Kind != ActNone {
		t.Fatalf("name enter should advance with no action, got %v", a.Kind)
	}
	if m.Buffer != "" {
		t.Errorf("name enter should clear buffer, got %q", m.Buffer)
	}

	// stage 1: repo → advances, no action.
	typeString(s, m, "myorg/myrepo")
	if a := s.Update(m, KeyEnter, 0); a.Kind != ActNone {
		t.Fatalf("repo enter should advance with no action, got %v", a.Kind)
	}
	if m.Buffer != "" {
		t.Errorf("repo enter should clear buffer, got %q", m.Buffer)
	}

	// stage 2: task → ActCreate with all fields set.
	typeString(s, m, "do the thing")
	a := s.Update(m, KeyEnter, 0)
	if a.Kind != ActCreate {
		t.Fatalf("task enter → ActCreate, got %v", a.Kind)
	}
	if a.Spec.Name != "agent-x" {
		t.Errorf("spec name = %q, want 'agent-x'", a.Spec.Name)
	}
	if a.Spec.Repo != "myorg/myrepo" {
		t.Errorf("spec repo = %q, want 'myorg/myrepo'", a.Spec.Repo)
	}
	if a.Spec.Task != "do the thing" {
		t.Errorf("spec task = %q, want 'do the thing'", a.Spec.Task)
	}
	// enter also pops back to the list and clears the buffer.
	if _, ok := m.Top().(*listScreen); !ok {
		t.Errorf("enter should pop back to the list, top is %T", m.Top())
	}
	if m.Buffer != "" {
		t.Errorf("enter should clear the buffer, got %q", m.Buffer)
	}
}

func TestInputCreateEmptyNameStays(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)
	a := s.Update(m, KeyEnter, 0) // empty name
	if a.Kind != ActNone {
		t.Errorf("empty name enter should be ActNone, got %v", a.Kind)
	}
	if _, ok := m.Top().(*inputScreen); !ok {
		t.Errorf("empty name should stay on input, top is %T", m.Top())
	}
}

func TestInputCreateEmptyRepoAllowed(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)

	typeString(s, m, "agent-y")
	s.Update(m, KeyEnter, 0) // commit name → repo stage

	// blank repo: enter advances (repo is optional).
	if a := s.Update(m, KeyEnter, 0); a.Kind != ActNone {
		t.Fatalf("blank repo enter should advance with no action, got %v", a.Kind)
	}

	typeString(s, m, "ship it")
	a := s.Update(m, KeyEnter, 0)
	if a.Kind != ActCreate {
		t.Fatalf("task enter → ActCreate, got %v", a.Kind)
	}
	if a.Spec.Repo != "" {
		t.Errorf("spec repo should be empty when skipped, got %q", a.Spec.Repo)
	}
	if a.Spec.Name != "agent-y" || a.Spec.Task != "ship it" {
		t.Errorf("spec = %+v, want name=agent-y task='ship it'", a.Spec)
	}
}

func TestInputCreateEmptyTaskStays(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)

	typeString(s, m, "agent-z")
	s.Update(m, KeyEnter, 0) // → repo stage
	s.Update(m, KeyEnter, 0) // blank repo → task stage

	a := s.Update(m, KeyEnter, 0) // empty task
	if a.Kind != ActNone {
		t.Errorf("empty task enter should be ActNone, got %v", a.Kind)
	}
	if _, ok := m.Top().(*inputScreen); !ok {
		t.Errorf("empty task should stay on input, top is %T", m.Top())
	}
}

func TestInputSendEnterReturnsNameAndTask(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(sendMode, "target-agent")
	m.push(s)
	for _, r := range "next step" {
		s.Update(m, KeyRune, r)
	}
	a := s.Update(m, KeyEnter, 0)
	if a.Kind != ActSend || a.Name != "target-agent" || a.Task != "next step" {
		t.Errorf("send-mode enter → ActSend(target-agent,next step), got %+v", a)
	}
}

func TestInputEmptyEnterDoesNothing(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)
	a := s.Update(m, KeyEnter, 0) // empty buffer
	if a.Kind != ActNone {
		t.Errorf("enter on empty buffer should be ActNone, got %v", a.Kind)
	}
	// stays on the input screen (didn't pop) so the user can type or esc.
	if _, ok := m.Top().(*inputScreen); !ok {
		t.Errorf("empty enter should stay on input, top is %T", m.Top())
	}
	_ = core.Agent{}
}
