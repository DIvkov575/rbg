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

func TestInputCreateEnterReturnsSpec(t *testing.T) {
	m := NewModel(nil)
	m.push(&listScreen{})
	s := newInputScreen(createMode, "")
	m.push(s)
	for _, r := range "do the thing" {
		s.Update(m, KeyRune, r)
	}
	a := s.Update(m, KeyEnter, 0)
	if a.Kind != ActCreate {
		t.Fatalf("create-mode enter → ActCreate, got %v", a.Kind)
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
