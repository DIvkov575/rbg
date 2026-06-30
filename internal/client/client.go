// Package client implements the laptop-side verbs: invoke rbg-agent over ssh
// with a structured argv in a single round-trip, and render the result. A
// connection failure surfaces as ssh exit 255 and is reported as a
// disconnection (no separate reachability probe).
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// runAgent execs rbg-agent <verb> over ssh in a SINGLE round-trip and returns
// its stdout and exit code. There is no separate reachability gate: a real
// connection failure surfaces as ssh exit 255 (see sshExitUnreachable), which
// callers map to the disconnection message. This halves per-command latency
// versus probing first (every command previously paid two ssh channels).
func runAgent(c *config.Config, r run.Runner, verb string, verbArgs []string) ([]byte, int) {
	remote := sshx.AgentArgs(c, verb, verbArgs)
	sshArgs := sshx.BuildSSHArgs(c, remote, sshx.Options{ConnectTimeout: true})
	out, code, _ := r.Run("ssh", sshArgs, nil)
	if code == sshExitUnreachable {
		fmt.Fprintf(os.Stderr, "cannot reach '%s' — disconnected\n", c.Host)
		return nil, 1
	}
	return out, code
}

// sshExitUnreachable is ssh(1)'s exit code when it cannot establish the
// connection (distinct from the agent's own 0/1/3 codes).
const sshExitUnreachable = 255

// Launch starts a bg agent on the desktop and prints the agent's reply. If name
// is empty the agent derives one from the task.
func Launch(c *config.Config, r run.Runner, out io.Writer, name, task string) int {
	args := []string{"--task", task}
	if name != "" {
		args = append([]string{"--name", name}, args...)
	}
	body, code := runAgent(c, r, "launch", args)
	out.Write(body)
	return code
}

// Send delivers a follow-up task; exit 3 propagates the agent's busy signal.
func Send(c *config.Config, r run.Runner, out io.Writer, name, task string) int {
	body, code := runAgent(c, r, "send", []string{"--id", name, "--task", task})
	if code == 3 {
		fmt.Fprintf(out, "rbg: session %q busy — a send is already running\n", name)
		return 3
	}
	out.Write(body)
	return code
}

// Read prints the named agent's transcript (already rendered by the agent).
func Read(c *config.Config, r run.Runner, out io.Writer, name string) int {
	text, err := FetchTranscript(c, r, name)
	if err != nil {
		fmt.Fprintf(out, "rbg: %v\n", err)
		return 1
	}
	fmt.Fprint(out, text)
	return 0
}

// Ls re-emits the session list as compact JSON, matching the agent's prior
// output (preserving the existing rbg ls output contract).
func Ls(c *config.Config, r run.Runner, out io.Writer) int {
	sessions, err := FetchSessions(c, r)
	if err != nil {
		fmt.Fprintf(out, "rbg: %v\n", err)
		return 1
	}
	json.NewEncoder(out).Encode(sessions)
	return 0
}

// Kill forgets an agent on the desktop (terminating any live child, keeping the
// transcript). Prints the agent's reply.
func Kill(c *config.Config, r run.Runner, out io.Writer, name string) int {
	body, code := runAgent(c, r, "kill", []string{"--id", name})
	out.Write(body)
	return code
}

// Ping reports reachability using the gate only.
func Ping(c *config.Config, r run.Runner, out io.Writer) int {
	if sshx.Reachable(c, r) {
		fmt.Fprintf(out, "%s: reachable\n", c.Host)
		return 0
	}
	fmt.Fprintf(out, "cannot reach '%s' — disconnected\n", c.Host)
	return 1
}
