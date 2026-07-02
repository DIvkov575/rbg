package ptybridge

import (
	"encoding/json"
	"errors"
	"io"
)

// Bridge runs the interactive attach loop over an already-connected pty socket
// conn. It:
//
//  1. reads the server's opening "hello" control frame,
//  2. sends an initial resize so the agent redraws at the caller's terminal size,
//  3. streams server pty output (KindData) to out and forwards keystrokes from
//     in to the server as KindData, until the agent exits or a side closes.
//
// It answers server "ping" frames with "pong" to keep the connection alive.
// Bridge returns nil on a clean detach/exit and a non-nil error only on an
// unexpected I/O failure.
func Bridge(conn io.ReadWriter, in io.Reader, out io.Writer, cols, rows int) error {
	// Handshake: the server speaks first with a hello control frame.
	kind, payload, err := ReadFrame(conn)
	if err != nil {
		return err
	}
	if kind != KindCtrl {
		return errors.New("ptybridge: expected hello control frame")
	}
	_ = payload // hello carries replPid/version; we don't need them to attach.

	// Ask the agent to redraw at our terminal size.
	if cols > 0 && rows > 0 {
		if err := WriteCtrl(conn, Ctrl{T: "resize", Cols: cols, Rows: rows}); err != nil {
			return err
		}
	}

	// Forward local keystrokes to the agent. Runs until stdin closes or a write
	// fails (e.g. the agent exited and closed the socket).
	inErr := make(chan error, 1)
	go func() { inErr <- pumpInput(conn, in) }()

	// Main loop: drain server frames to the terminal until exit/EOF.
	for {
		kind, payload, err := ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		switch kind {
		case KindData:
			if _, werr := out.Write(payload); werr != nil {
				return werr
			}
		case KindCtrl:
			c, cerr := decodeCtrl(payload)
			if cerr != nil {
				continue // ignore malformed control frames rather than dying
			}
			switch c.T {
			case "ping":
				_ = WriteCtrl(conn, Ctrl{T: "pong"})
			case "exit":
				return nil // the agent process ended; detach cleanly
			}
		}
	}
}

// pumpInput copies keystrokes from in and frames them as pty stdin to conn. A
// closed socket (agent gone) surfaces as a write error, which ends the pump.
func pumpInput(conn io.Writer, in io.Reader) error {
	buf := make([]byte, 4096)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			if werr := WriteData(conn, buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			return err
		}
	}
}

func decodeCtrl(payload []byte) (Ctrl, error) {
	var c Ctrl
	err := json.Unmarshal(payload, &c)
	return c, err
}
