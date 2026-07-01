// Package run defines the subprocess-runner seam used across rbg, so all
// command logic can be unit-tested without spawning real processes.
package run

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
)

// Result is a canned subprocess outcome for the Recording test runner.
type Result struct {
	Stdout []byte
	Stderr []byte
	Code   int
	Err    error
}

// Call records one invocation made through a Runner.
type Call struct {
	Name string
	Args []string
}

// Runner abstracts running a subprocess. Implementations: Exec (real) and
// Recording (test stub).
type Runner interface {
	Run(name string, args []string, stdin io.Reader) (stdout []byte, code int, err error)
}

// Exec is the real runner backed by os/exec.
type Exec struct{}

func (Exec) Run(name string, args []string, stdin io.Reader) ([]byte, int, error) {
	cmd := exec.Command(name, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	// Capture stdout and stderr separately. Callers parse stdout (JSON replies,
	// `claude agents` output) and must not see it polluted by warnings a command
	// prints to stderr on success. But callers format FAILURES as
	// "<cmd> exited N: <out>", and the diagnostic reason (git's "Not possible to
	// fast-forward", claude's credential error, etc.) goes to stderr — so on a
	// non-zero exit we append stderr to the returned bytes to make the message
	// actionable rather than blank.
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
		err = nil // exit code carries the signal; not a Go-level error
	}
	if code != 0 && errBuf.Len() > 0 {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.Write(errBuf.Bytes())
	}
	return out.Bytes(), code, err
}

// Recording is a test Runner: records calls, returns a Result chosen by the
// first BySubstring key found in the joined args, else Default.
type Recording struct {
	Calls       []Call
	BySubstring map[string]Result
	Default     Result
}

func (r *Recording) Run(name string, args []string, stdin io.Reader) ([]byte, int, error) {
	r.Calls = append(r.Calls, Call{Name: name, Args: args})
	joined := joinArgs(args)
	for sub, res := range r.BySubstring {
		if strings.Contains(joined, sub) {
			return res.Stdout, res.Code, res.Err
		}
	}
	return r.Default.Stdout, r.Default.Code, r.Default.Err
}

func joinArgs(args []string) string { return strings.Join(args, " ") }

var _ Runner = Exec{}
var _ Runner = (*Recording)(nil)
