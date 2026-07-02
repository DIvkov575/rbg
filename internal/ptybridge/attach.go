package ptybridge

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
)

// Attach resolves the live worker for session id from the daemon roster under
// home, dials its pty socket, puts the controlling terminal (stdin/stdout) in
// raw mode, and bridges the two until the agent exits or the user detaches.
//
// It is meant to run on the machine hosting the agent — locally, or on the
// desktop under `ssh -t` where stdin/stdout are the user's real terminal. The
// caller supplies stdin/stdout/stderr so it stays testable.
func Attach(home, id string, stdin io.Reader, stdout, stderr io.Writer) error {
	w, err := FindWorker(home, id)
	if err != nil {
		return err
	}

	conn, err := net.DialTimeout("unix", w.PtySock, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect pty socket: %w", err)
	}
	defer conn.Close()

	cols, rows := terminalSize(stdout)

	// Put the controlling terminal in raw mode so keystrokes (arrows, ctrl-C,
	// etc.) pass through to the agent unbuffered. Only attempt this when stdin is
	// a real terminal; in tests/pipes it is not, and we bridge without it.
	if restore := makeRaw(stdin); restore != nil {
		defer restore()
	}

	fmt.Fprintf(stderr, "rbg: attached to %s — the agent is live. Ctrl-\\ then Ctrl-C detaches ssh.\r\n", short(w.SessionID))
	return Bridge(conn, stdin, stdout, cols, rows)
}

// terminalSize returns the size of the terminal backing out, or 0,0 if out is
// not a terminal (the agent then keeps its current size).
func terminalSize(out io.Writer) (cols, rows int) {
	f, ok := out.(*os.File)
	if !ok {
		return 0, 0
	}
	w, h, err := term.GetSize(f.Fd())
	if err != nil {
		return 0, 0
	}
	return w, h
}

// makeRaw switches in's terminal to raw mode and returns a restore func, or nil
// if in is not a real terminal.
func makeRaw(in io.Reader) func() {
	f, ok := in.(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return nil
	}
	state, err := term.MakeRaw(f.Fd())
	if err != nil {
		return nil
	}
	return func() { _ = term.Restore(f.Fd(), state) }
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
