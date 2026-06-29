// Package sshx builds ssh invocations for the rbg client. SSH is transport
// only: it execs rbg-agent (or claude, for attach) directly with a structured
// argv — no remote shell, so nothing is shell-interpolated.
package sshx

import (
	"fmt"
	"os"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

// Options tunes a single ssh invocation.
type Options struct {
	TTY   bool // allocate a tty (-t) for interactive attach
	Batch bool // BatchMode + ConnectTimeout, for the reachability probe
}

// BuildSSHArgs returns the argv for `ssh` (excluding the leading "ssh"):
// [opts...] <host> <remote argv...>. The remote argv is passed as separate
// arguments; OpenSSH forwards them to the remote exec without a shell when
// invoked this way by os/exec (we never wrap them in `sh -c`).
func BuildSSHArgs(c *config.Config, remote []string, o Options) []string {
	var args []string
	if o.Batch {
		args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout=5")
	}
	if o.TTY {
		args = append(args, "-t")
	}
	args = append(args, c.SSHOpts...)
	args = append(args, c.Host)
	args = append(args, remote...)
	return args
}

// AgentArgs builds the remote argv that invokes rbg-agent for a verb. When CWD
// is set it is passed as a structured --cwd flag (not a shell `cd`).
func AgentArgs(c *config.Config, verb string, verbArgs []string) []string {
	out := []string{c.AgentPath}
	if c.CWD != "" {
		out = append(out, "--cwd", c.CWD)
	}
	out = append(out, verb)
	out = append(out, verbArgs...)
	return out
}

// Reachable runs the connection-gate probe. True iff ssh ... true exits 0.
func Reachable(c *config.Config, r run.Runner) bool {
	args := BuildSSHArgs(c, []string{"true"}, Options{Batch: true})
	_, code, err := r.Run("ssh", args, nil)
	return err == nil && code == 0
}

// EnsureReachable prints the v1 disconnection message and exits 1 if the host
// is unreachable.
func EnsureReachable(c *config.Config, r run.Runner) {
	if !Reachable(c, r) {
		fmt.Fprintf(os.Stderr, "cannot reach '%s' — disconnected\n", c.Host)
		os.Exit(1)
	}
}
