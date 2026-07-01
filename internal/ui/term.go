package ui

import (
	"io"
	"os"
)

// Stdio bundles the loop's I/O endpoints (injectable for testing).
type Stdio struct {
	In  io.Reader
	Out io.Writer
}

// DefaultStdio uses the process terminal.
func DefaultStdio() Stdio { return Stdio{In: os.Stdin, Out: os.Stdout} }

// readRaw reads one input chunk; nil/empty means EOF (treated as quit).
func readRaw(r io.Reader) []byte {
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil || n == 0 {
		return nil
	}
	return buf[:n]
}
