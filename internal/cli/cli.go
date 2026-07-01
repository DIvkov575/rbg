// Package cli is rbg's scriptable command layer: it parses non-interactive
// verbs, calls the Engine, and renders results. It depends on the Ops interface
// (satisfied by *engine.Engine) so it is unit-testable with a fake — no real
// SSH or claude. cmd/rbg builds a real Engine and routes verbs through Dispatch.
package cli

import (
	"fmt"
	"io"

	"github.com/divkov575/rbg/internal/core"
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

// Dispatch parses args (verb + operands), invokes ops, writes output to out,
// and returns a process exit code (0 = success, non-zero = error/usage).
func Dispatch(args []string, ops Ops, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(out, usage())
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "ls":
		return doLs(ops, out)
	case "create":
		return doCreate(rest, ops, out)
	case "run":
		return doName(rest, out, ops.Run)
	case "adopt":
		return doName(rest, out, ops.Adopt)
	case "kill":
		return doName(rest, out, ops.Kill)
	default:
		fmt.Fprintf(out, "rbg: unknown command %q\n\n%s", verb, usage())
		return 2
	}
}

// doLs renders the reconciled inventory. A degraded list (unreachable machine)
// still renders the usable agents, prefixed with a warning, and exits non-zero.
func doLs(ops Ops, out io.Writer) int {
	agents, err := ops.List()
	if err != nil {
		fmt.Fprintf(out, "warning: inventory may be incomplete: %v\n", err)
	}
	fmt.Fprint(out, renderAgents(agents))
	if err != nil {
		return 1
	}
	return 0
}

// doCreate stages a held task from `create <name> <repo> <task>`.
func doCreate(rest []string, ops Ops, out io.Writer) int {
	if len(rest) != 3 {
		fmt.Fprintf(out, "usage: rbg create <name> <repo> <task>\n")
		return 2
	}
	if _, err := ops.Create(core.Agent{Name: rest[0], Repo: rest[1], Task: rest[2]}); err != nil {
		fmt.Fprintf(out, "rbg: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "created %q (held)\n", rest[0])
	return 0
}

// doName runs a one-name operation (run/adopt/kill), mapping a missing name to a
// usage error and an engine error to exit 1.
func doName(rest []string, out io.Writer, op func(string) error) int {
	if len(rest) != 1 {
		fmt.Fprintf(out, "usage: rbg <verb> <name>\n")
		return 2
	}
	if err := op(rest[0]); err != nil {
		fmt.Fprintf(out, "rbg: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "ok: %s\n", rest[0])
	return 0
}

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
`
}
