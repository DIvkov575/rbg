// Package cli is rbg's scriptable command layer: it parses non-interactive
// verbs, calls the Engine, and renders results. It depends on the Ops interface
// (satisfied by *engine.Engine) so it is unit-testable with a fake — no real
// SSH or claude. cmd/rbg builds a real Engine and routes verbs through Dispatch.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
	"github.com/divkov575/rbg/internal/render"
)

// Ops is the Engine surface the CLI drives. *engine.Engine satisfies it.
type Ops interface {
	List() ([]core.Agent, error)
	Create(spec core.Agent) (core.Agent, error)
	Run(name string) error
	Send(name, task string) error
	Read(name string) ([]byte, error)
	Kill(name string) error
	Adopt(name string) error
}

// Exit codes: 0 ok, 1 engine error, 2 usage/unknown-verb, 3 agent busy.
// Data (agent lists, transcripts) goes to out; errors, warnings, and usage go
// to errOut, so `rbg ls > file` captures only data.
func Dispatch(args []string, ops Ops, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errOut, usage())
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return doLs(ops, out, errOut)
	case "create":
		return doCreate(rest, ops, out, errOut)
	case "run":
		return doName(rest, out, errOut, ops.Run)
	case "adopt":
		return doName(rest, out, errOut, ops.Adopt)
	case "kill":
		return doName(rest, out, errOut, ops.Kill)
	case "send":
		return doSend(rest, ops, out, errOut)
	case "read":
		return doRead(rest, ops, out, errOut)
	default:
		fmt.Fprintf(errOut, "rbg: unknown command %q\n\n%s", verb, usage())
		return 2
	}
}

// doLs renders the reconciled inventory. A degraded list (unreachable machine)
// still renders the usable agents, with a warning to errOut, and exits non-zero.
func doLs(ops Ops, out, errOut io.Writer) int {
	agents, err := ops.List()
	if err != nil {
		fmt.Fprintf(errOut, "warning: inventory may be incomplete: %v\n", err)
	}
	fmt.Fprint(out, renderAgents(agents))
	if err != nil {
		return 1
	}
	return 0
}

// doCreate stages a held task from `create <name> <repo> <task>`.
func doCreate(rest []string, ops Ops, out, errOut io.Writer) int {
	if len(rest) != 3 {
		fmt.Fprintf(errOut, "usage: rbg create <name> <repo> <task>\n")
		return 2
	}
	if _, err := ops.Create(core.Agent{Name: rest[0], Repo: rest[1], Task: rest[2]}); err != nil {
		fmt.Fprintf(errOut, "rbg: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "created %q (held)\n", rest[0])
	return 0
}

// doName runs a one-name operation (run/adopt/kill), mapping a missing name to a
// usage error and an engine error to exit 1.
func doName(rest []string, out, errOut io.Writer, op func(string) error) int {
	if len(rest) != 1 {
		fmt.Fprintf(errOut, "usage: rbg <verb> <name>\n")
		return 2
	}
	if err := op(rest[0]); err != nil {
		fmt.Fprintf(errOut, "rbg: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "ok: %s\n", rest[0])
	return 0
}

// doSend delivers a follow-up task from `send <name> <task>`. A busy agent is
// reported clearly (host.ErrBusy), distinct from other failures.
func doSend(rest []string, ops Ops, out, errOut io.Writer) int {
	if len(rest) != 2 {
		fmt.Fprintf(errOut, "usage: rbg send <name> <task>\n")
		return 2
	}
	if err := ops.Send(rest[0], rest[1]); err != nil {
		if errors.Is(err, host.ErrBusy) {
			fmt.Fprintf(errOut, "rbg: %q is busy — a send is already running\n", rest[0])
			return 3
		}
		fmt.Fprintf(errOut, "rbg: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "sent to %s\n", rest[0])
	return 0
}

// doRead prints an agent's transcript, rendering the raw JSONL to human text.
func doRead(rest []string, ops Ops, out, errOut io.Writer) int {
	if len(rest) != 1 {
		fmt.Fprintf(errOut, "usage: rbg read <name>\n")
		return 2
	}
	data, err := ops.Read(rest[0])
	if err != nil {
		fmt.Fprintf(errOut, "rbg: %v\n", err)
		return 1
	}
	render.Stream(strings.Split(string(data), "\n"), out)
	return 0
}

// Usage returns the full command help text.
func Usage() string { return usage() }

// usage returns the scriptable-verb help text.
func usage() string {
	return `rbg — remote Claude agent management

Commands:
  ls                       list all agents (both machines)
  create <name> <repo> <task>   stage a held task
  run <name>               launch (or re-run) a staged agent, sync-first
  send <name> <task>       send a follow-up to a running agent
  read <name>              print an agent's transcript
  kill <name>              stop an agent (keeps transcript)
  adopt <name>             manage an agent started outside rbg
  dash                     open the interactive dashboard (default)
  deploy                   build & install the agent on the desktop
  attach <name>            attach interactively (TTY)
  ping                     check reachability
  help                     show this help

Configuration (env or ~/.rbg.conf; env wins):
  RBG_HOST (required), RBG_CWD, RBG_SSH, RBG_AGENT_PATH, RBG_MUX,
  RBG_CONTROL_PATH, RBG_CONTROL_PERSIST
`
}
