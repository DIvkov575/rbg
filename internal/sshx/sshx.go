// Package sshx builds ssh invocations for the rbg client. OpenSSH concatenates
// the remote argv into a single string that the desktop login shell re-parses,
// so every remote token is POSIX single-quoted (see RemoteCommand/QuoteToken)
// to keep arguments literal and prevent shell injection.
package sshx

import (
	"fmt"
	"os"
	"strings"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

// Options tunes a single ssh invocation.
type Options struct {
	TTY            bool // allocate a tty (-t) for interactive attach
	Batch          bool // BatchMode + ConnectTimeout, for the reachability probe
	ConnectTimeout bool // BatchMode + ConnectTimeout for a normal command (fail fast if down)
}

// QuoteToken POSIX single-quotes a token so the remote login shell treats it as
// a single literal argument. Embedded single quotes are escaped as '\”; the
// empty string becomes ”.
func QuoteToken(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RemoteCommand collapses a remote argv into a single shell-safe command string.
// This is the ONE place quoting happens: OpenSSH joins the remote arguments into
// a single string that the desktop login shell ($SHELL -c) re-parses, so every
// token must be quoted here to prevent shell injection.
func RemoteCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, tok := range argv {
		quoted[i] = QuoteToken(tok)
	}
	return strings.Join(quoted, " ")
}

// userSetControl reports whether the user's own SSH options already configure
// connection multiplexing, so rbg should not inject its own (theirs wins).
func userSetControl(opts []string) bool {
	for _, o := range opts {
		if strings.HasPrefix(o, "Control") {
			return true
		}
	}
	return false
}

// BuildSSHArgs returns the argv for `ssh` (excluding the leading "ssh"):
// [opts...] <host> <remote-command>. The remote argv is collapsed into a SINGLE
// shell-quoted string element via RemoteCommand, because OpenSSH concatenates
// the remote arguments and the desktop login shell re-parses the result.
func BuildSSHArgs(c *config.Config, remote []string, o Options) []string {
	var args []string
	if o.Batch || o.ConnectTimeout {
		args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout=5")
	}
	if o.TTY {
		args = append(args, "-t")
	}
	if c.Mux && !userSetControl(c.SSHOpts) {
		args = append(args,
			"-o", "ControlMaster=auto",
			"-o", "ControlPath="+c.ControlPath,
			"-o", "ControlPersist="+c.ControlPersist,
		)
	}
	args = append(args, c.SSHOpts...)
	args = append(args, c.Host)
	args = append(args, RemoteCommand(remote))
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
