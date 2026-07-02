// Package ptybridge speaks the Claude Code background-agent pty-socket protocol
// so a live bg agent's terminal can be attached to interactively.
//
// The daemon publishes live workers in ~/.claude/daemon/roster.json, each with a
// unix-socket path (ptySock). On connect the server sends a framed "hello"; from
// then on it streams the agent's terminal as raw-byte frames and accepts the
// user's keystrokes back the same way. Framing (verified against claude
// 2.1.197):
//
//	[4-byte big-endian payload length][1 kind byte][payload...]
//
//	kind 0 (KindData) — raw pty bytes: stdin (client→server) / stdout (server→client)
//	kind 1 (KindCtrl) — JSON control message: {"t":"hello"|"ping"|"pong"|"exit"
//	                    |"live"|"resize"|"auth", ...}
//
// The 4-byte length counts the payload only, NOT the kind byte (verified by
// parsing a live worker's stream: the hello frame's length was 48 for its
// 48-byte JSON payload, with the kind byte separate). The JSON control frame's
// payload is UTF-8 JSON. Output
// streams to any connected client without a token; input is ungated on fleet
// workers (the server only rejects input when an ephemeral, spawn-time token is
// configured, which fleet workers do not set).
package ptybridge

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Frame kinds, matching the daemon's wire constants.
const (
	KindData byte = 0 // raw pty bytes (stdin/stdout)
	KindCtrl byte = 1 // JSON control message
)

const headerLen = 5 // 4-byte length + 1-byte kind

// maxFrame bounds a single frame's payload so a corrupt/hostile length prefix
// can't make us allocate unboundedly. A pty burst is far smaller than this.
const maxFrame = 8 << 20 // 8 MiB

// Ctrl is a JSON control message. Only the fields we read or send are modeled;
// unknown fields are ignored on decode.
type Ctrl struct {
	T       string `json:"t"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Token   string `json:"token,omitempty"`
	Code    int    `json:"code,omitempty"`
	Signal  string `json:"signal,omitempty"`
	ReplPid int    `json:"replPid,omitempty"`
}

// WriteData frames raw pty bytes (KindData) and writes them to w.
func WriteData(w io.Writer, p []byte) error {
	return writeFrame(w, KindData, p)
}

// WriteCtrl marshals c as JSON and writes it as a control frame (KindCtrl).
func WriteCtrl(w io.Writer, c Ctrl) error {
	payload, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return writeFrame(w, KindCtrl, payload)
}

func writeFrame(w io.Writer, kind byte, payload []byte) error {
	hdr := make([]byte, headerLen)
	// Length counts the payload only; the kind byte follows separately.
	binary.BigEndian.PutUint32(hdr[:4], uint32(len(payload)))
	hdr[4] = kind
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads one frame from r, returning its kind and payload. It returns
// io.EOF when the peer closes cleanly between frames.
func ReadFrame(r io.Reader) (kind byte, payload []byte, err error) {
	hdr := make([]byte, headerLen)
	if _, err = io.ReadFull(r, hdr); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:4])
	kind = hdr[4]
	if n > maxFrame {
		return kind, nil, fmt.Errorf("ptybridge: frame length %d exceeds max %d", n, maxFrame)
	}
	if n == 0 {
		return kind, nil, nil
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(r, payload); err != nil {
		return kind, nil, err
	}
	return kind, payload, nil
}
