// Package client implements the laptop-side verbs: run the connection gate,
// invoke rbg-agent over ssh with a structured argv, and render the result.
package client

import (
	"fmt"
	"io"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// runAgent runs the connection gate then execs rbg-agent <verb> over ssh,
// returning its stdout and exit code.
func runAgent(c *config.Config, r run.Runner, verb string, verbArgs []string) ([]byte, int) {
	sshx.EnsureReachable(c, r)
	remote := sshx.AgentArgs(c, verb, verbArgs)
	sshArgs := sshx.BuildSSHArgs(c, remote, sshx.Options{})
	out, code, _ := r.Run("ssh", sshArgs, nil)
	return out, code
}

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

// Read prints the session transcript (already rendered by the agent).
func Read(c *config.Config, r run.Runner, out io.Writer, name string) int {
	body, code := runAgent(c, r, "read", []string{"--id", name})
	out.Write(body)
	return code
}

// Ls prints the desktop's session list.
func Ls(c *config.Config, r run.Runner, out io.Writer) int {
	body, code := runAgent(c, r, "ls", nil)
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
