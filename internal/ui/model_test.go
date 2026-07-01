package ui

import (
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func TestViewModeCycle(t *testing.T) {
	// ctrl-s cycles Remote → Local → Combined → Project → Remote.
	order := []ViewMode{ViewRemote, ViewLocal, ViewCombined, ViewProject}
	v := ViewRemote
	for i := 1; i <= 4; i++ {
		v = v.Next()
		want := order[i%4]
		if v != want {
			t.Errorf("after %d cycles: got %v, want %v", i, v, want)
		}
	}
}

func TestStackPushPopTop(t *testing.T) {
	base := &listScreen{}
	m := NewModel(nil)
	m.push(base)
	if m.Top() != base {
		t.Fatalf("Top after push should be base")
	}
	child := &inputScreen{}
	m.push(child)
	if m.Top() != child {
		t.Errorf("Top after second push should be child")
	}
	m.pop()
	if m.Top() != base {
		t.Errorf("Top after pop should be base again")
	}
	// popping the last screen leaves Top nil (loop treats nil as quit).
	m.pop()
	if m.Top() != nil {
		t.Errorf("Top after popping all should be nil")
	}
}

func TestSelectedAgent(t *testing.T) {
	agents := []core.Agent{
		{Name: "a", Where: core.Remote},
		{Name: "b", Where: core.Remote},
	}
	m := NewModel(agents)
	m.View = ViewRemote
	m.Cursor = 1
	sel, ok := m.Selected()
	if !ok || sel.Name != "b" {
		t.Errorf("Selected at cursor 1 = %+v ok=%v, want b", sel, ok)
	}
	// Out-of-range cursor → no selection (defensive).
	m.Cursor = 99
	if _, ok := m.Selected(); ok {
		t.Errorf("out-of-range cursor should yield no selection")
	}
	// Empty inventory → no selection.
	if _, ok := NewModel(nil).Selected(); ok {
		t.Errorf("empty inventory should yield no selection")
	}
}

func TestVisibleAgentsPerView(t *testing.T) {
	agents := []core.Agent{
		{Name: "loc", Where: core.Local},
		{Name: "rem", Where: core.Remote},
	}
	m := NewModel(agents)
	m.View = ViewLocal
	if v := m.Visible(); len(v) != 1 || v[0].Name != "loc" {
		t.Errorf("Local view = %+v, want [loc]", v)
	}
	m.View = ViewRemote
	if v := m.Visible(); len(v) != 1 || v[0].Name != "rem" {
		t.Errorf("Remote view = %+v, want [rem]", v)
	}
	m.View = ViewCombined
	if v := m.Visible(); len(v) != 2 {
		t.Errorf("Combined view should show both, got %d", len(v))
	}
}
