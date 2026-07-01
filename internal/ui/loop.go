package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// Ops is the engine surface the dashboard drives. *engine.Engine satisfies it
// (same method set as the CLI's Ops), so the loop needs no real SSH in tests.
type Ops interface {
	List() ([]core.Agent, error)
	Create(spec core.Agent) (core.Agent, error)
	Run(name string) error
	Send(name, task string) error
	Read(name string) ([]byte, error)
	Kill(name string) error
	Adopt(name string) error
}

// refresh re-pulls the reconciled inventory into the model, surfacing a
// degradation error as a status line but still showing whatever came back.
func refresh(m *Model, ops Ops) {
	agents, err := ops.List()
	m.SetAgents(agents)
	if err != nil {
		m.Status = "inventory may be incomplete: " + err.Error()
	}
}

// applyAction fulfills one Action against the engine and returns true when the
// loop should exit. Mutating actions refresh the inventory so the list reflects
// the new state; ActRead pushes a pager over the fetched transcript. Errors go
// to the status line rather than aborting the dashboard.
func applyAction(m *Model, ops Ops, act Action) bool {
	switch act.Kind {
	case ActQuit:
		return true
	case ActRefresh:
		refresh(m, ops)
	case ActRun:
		if err := ops.Run(act.Name); err != nil {
			m.Status = "run failed: " + err.Error()
		} else {
			m.Status = "ran " + act.Name
		}
		refresh(m, ops)
	case ActSend:
		if err := ops.Send(act.Name, act.Task); err != nil {
			m.Status = "send failed: " + err.Error()
		} else {
			m.Status = "sent to " + act.Name
		}
		refresh(m, ops)
	case ActKill:
		if err := ops.Kill(act.Name); err != nil {
			m.Status = "kill failed: " + err.Error()
		} else {
			m.Status = "killed " + act.Name
		}
		refresh(m, ops)
	case ActAdopt:
		if err := ops.Adopt(act.Name); err != nil {
			m.Status = "adopt failed: " + err.Error()
		} else {
			m.Status = "adopted " + act.Name
		}
		refresh(m, ops)
	case ActCreate:
		if _, err := ops.Create(act.Spec); err != nil {
			m.Status = "create failed: " + err.Error()
		} else {
			m.Status = "created a held agent"
		}
		refresh(m, ops)
	case ActRead:
		data, err := ops.Read(act.Name)
		if err != nil {
			m.Status = "read failed: " + err.Error()
			return false
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		m.push(newPagerScreen("transcript: "+act.Name, lines))
	}
	return false
}

// Run drives the dashboard until the user quits. It pulls the initial
// inventory, enters raw mode, and on each key: decodes it, hands it to the top
// screen's Update, fulfills the returned Action, and redraws. EOF quits.
func Run(ops Ops, io Stdio) error {
	m := NewModel(nil)
	refresh(m, ops)
	w, h := termSize(os.Stdin.Fd())
	m.W, m.H = w, h

	restore, err := rawMode(os.Stdin.Fd())
	if err != nil {
		return err
	}
	defer restore()

	draw(io.Out, m)
	for {
		raw := readRaw(io.In)
		if raw == nil {
			return nil // EOF → quit
		}
		top := m.Top()
		if top == nil {
			return nil
		}
		k, r := DecodeKey(raw)
		act := top.Update(m, k, r)
		if applyAction(m, ops, act) {
			return nil
		}
		w, h := termSize(os.Stdin.Fd())
		m.W, m.H = w, h
		draw(io.Out, m)
	}
}

const clearScreen = "\x1b[2J\x1b[H"

// draw clears the screen and renders the top screen.
func draw(out io.Writer, m *Model) {
	top := m.Top()
	if top == nil {
		return
	}
	fmt.Fprint(out, clearScreen)
	fmt.Fprint(out, top.View(m))
}
