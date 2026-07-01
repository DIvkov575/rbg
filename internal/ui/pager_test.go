package ui

import (
	"strings"
	"testing"
)

func TestPagerScrollsAndClampsToBounds(t *testing.T) {
	m := NewModel(nil)
	m.H = 6 // small window
	p := newPagerScreen("transcript", []string{"l0", "l1", "l2", "l3", "l4", "l5", "l6", "l7"})
	m.push(p)

	// scrolling up at the top is a no-op (offset stays 0)
	p.Update(m, KeyUp, 0)
	if p.offset != 0 {
		t.Errorf("offset = %d, want 0 (already at top)", p.offset)
	}
	// down advances the offset
	p.Update(m, KeyDown, 0)
	if p.offset != 1 {
		t.Errorf("offset after one down = %d, want 1", p.offset)
	}
	// 'j'/'k' behave like down/up
	p.Update(m, KeyRune, 'j')
	if p.offset != 2 {
		t.Errorf("offset after j = %d, want 2", p.offset)
	}
	p.Update(m, KeyRune, 'k')
	if p.offset != 1 {
		t.Errorf("offset after k = %d, want 1", p.offset)
	}
}

func TestPagerEscPops(t *testing.T) {
	m := NewModel(nil)
	base := m.Top()
	p := newPagerScreen("t", []string{"x"})
	m.push(p)
	if m.Top() != p {
		t.Fatal("pager should be on top after push")
	}
	act := p.Update(m, KeyEsc, 0)
	if act.Kind != ActNone {
		t.Errorf("Esc should return ActNone, got %v", act.Kind)
	}
	if m.Top() != base {
		t.Errorf("Esc should pop back to the base screen")
	}
}

func TestPagerViewShowsTitleAndLines(t *testing.T) {
	m := NewModel(nil)
	m.H = 10
	p := newPagerScreen("my transcript", []string{"hello", "world"})
	out := p.View(m)
	if !strings.Contains(out, "my transcript") || !strings.Contains(out, "hello") {
		t.Errorf("pager view missing title/content: %q", out)
	}
}
