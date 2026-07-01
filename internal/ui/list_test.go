package ui

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func listModel() *Model {
	m := NewModel([]core.Agent{
		{Name: "r1", Where: core.Remote, State: core.Running, Origin: core.Managed},
		{Name: "r2", Where: core.Remote, State: core.Done, Origin: core.Foreign},
	})
	m.push(&listScreen{})
	return m
}

func TestListNavigation(t *testing.T) {
	m := listModel()
	s := m.Top()
	s.Update(m, KeyDown, 0)
	if m.Cursor != 1 {
		t.Errorf("down → cursor 1, got %d", m.Cursor)
	}
	s.Update(m, KeyDown, 0) // clamp at last
	if m.Cursor != 1 {
		t.Errorf("down at end should clamp at 1, got %d", m.Cursor)
	}
	s.Update(m, KeyUp, 0)
	if m.Cursor != 0 {
		t.Errorf("up → cursor 0, got %d", m.Cursor)
	}
	s.Update(m, KeyUp, 0) // clamp at 0
	if m.Cursor != 0 {
		t.Errorf("up at top should clamp at 0, got %d", m.Cursor)
	}
	// j/k are down/up too (KeyRune 'j'/'k').
	s.Update(m, KeyRune, 'j')
	if m.Cursor != 1 {
		t.Errorf("j → cursor 1, got %d", m.Cursor)
	}
	s.Update(m, KeyRune, 'k')
	if m.Cursor != 0 {
		t.Errorf("k → cursor 0, got %d", m.Cursor)
	}
}

func TestListCycleViewResetsCursor(t *testing.T) {
	m := listModel()
	m.Cursor = 1
	m.Top().Update(m, KeyCycleView, 0)
	if m.View != ViewLocal {
		t.Errorf("ctrl-s should advance to Local, got %v", m.View)
	}
	// Local view is empty here; cursor must clamp to 0 (not dangle at 1).
	if m.Cursor != 0 {
		t.Errorf("cycling to an empty view should reset cursor to 0, got %d", m.Cursor)
	}
}

func TestListActionKeys(t *testing.T) {
	m := listModel() // cursor 0 → r1 (managed, running)
	// Quit
	if a := m.Top().Update(m, KeyQuit, 0); a.Kind != ActQuit {
		t.Errorf("q → ActQuit, got %v", a.Kind)
	}
	// Refresh
	if a := m.Top().Update(m, KeyRefresh, 0); a.Kind != ActRefresh {
		t.Errorf("r → ActRefresh, got %v", a.Kind)
	}
	// Enter → Read the selected agent
	m.Cursor = 0
	if a := m.Top().Update(m, KeyEnter, 0); a.Kind != ActRead || a.Name != "r1" {
		t.Errorf("enter → ActRead(r1), got %+v", a)
	}
	// 'x' → Kill the selected agent (x avoids the k/j nav collision)
	if a := m.Top().Update(m, KeyRune, 'x'); a.Kind != ActKill || a.Name != "r1" {
		t.Errorf("x → ActKill(r1), got %+v", a)
	}
	// 'g' → Run the selected agent (go)
	if a := m.Top().Update(m, KeyRune, 'g'); a.Kind != ActRun || a.Name != "r1" {
		t.Errorf("g → ActRun(r1), got %+v", a)
	}
}

func TestListAdoptOnlyForForeign(t *testing.T) {
	m := listModel()
	m.Cursor = 0 // r1 is managed
	if a := m.Top().Update(m, KeyRune, 'A'); a.Kind != ActNone {
		t.Errorf("adopt on a managed agent should be a no-op, got %v", a.Kind)
	}
	m.Cursor = 1 // r2 is foreign
	if a := m.Top().Update(m, KeyRune, 'A'); a.Kind != ActAdopt || a.Name != "r2" {
		t.Errorf("A on a foreign agent → ActAdopt(r2), got %+v", a)
	}
}

func TestListNewPushesInputScreen(t *testing.T) {
	m := listModel()
	before := m.Top()
	m.Top().Update(m, KeyRune, 'n')
	if m.Top() == before {
		t.Errorf("n should push a new screen (input), top unchanged")
	}
	if _, ok := m.Top().(*inputScreen); !ok {
		t.Errorf("n should push an *inputScreen, got %T", m.Top())
	}
}
