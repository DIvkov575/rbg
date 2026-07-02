package ptybridge

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestBridgeStreamsOutputAndForwardsInput drives Bridge against a fake server on
// one end of net.Pipe: the server sends hello, expects our resize, streams a pty
// output frame, reads a keystroke we typed, pings (expects pong), then sends
// exit. Bridge should return nil, having written the output to stdout.
func TestBridgeStreamsOutputAndForwardsInput(t *testing.T) {
	client, server := net.Pipe()
	stdin := bytes.NewBufferString("q") // one keystroke to forward
	var stdout bytes.Buffer

	var serverErr error
	var pongSeen, resizeSeen, keySeen bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		// 1. hello (server speaks first)
		if err := WriteCtrl(server, Ctrl{T: "hello", ReplPid: 1}); err != nil {
			serverErr = err
			return
		}
		// 2. expect resize from client
		kind, payload, err := ReadFrame(server)
		if err != nil {
			serverErr = err
			return
		}
		if kind == KindCtrl {
			if c, _ := decodeCtrl(payload); c.T == "resize" && c.Cols == 100 && c.Rows == 30 {
				resizeSeen = true
			}
		}
		// 3. stream a pty output frame
		if err := WriteData(server, []byte("SCREEN")); err != nil {
			serverErr = err
			return
		}
		// 4. read the forwarded keystroke
		kind, payload, err = ReadFrame(server)
		if err != nil {
			serverErr = err
			return
		}
		if kind == KindData && string(payload) == "q" {
			keySeen = true
		}
		// 5. ping → expect pong
		if err := WriteCtrl(server, Ctrl{T: "ping"}); err != nil {
			serverErr = err
			return
		}
		kind, payload, err = ReadFrame(server)
		if err != nil {
			serverErr = err
			return
		}
		if kind == KindCtrl {
			if c, _ := decodeCtrl(payload); c.T == "pong" {
				pongSeen = true
			}
		}
		// 6. exit → Bridge returns
		_ = WriteCtrl(server, Ctrl{T: "exit"})
	}()

	// stdin is a plain buffer (not a terminal), so no raw mode — bridge directly.
	brErr := Bridge(client, blockAfter(stdin), &stdout, 100, 30)
	client.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server goroutine did not finish")
	}

	if serverErr != nil {
		t.Fatalf("server error: %v", serverErr)
	}
	if brErr != nil {
		t.Fatalf("Bridge returned error: %v", brErr)
	}
	if !resizeSeen {
		t.Error("server never saw the initial resize")
	}
	if !keySeen {
		t.Error("server never saw the forwarded keystroke")
	}
	if !pongSeen {
		t.Error("server never got a pong for its ping")
	}
	if !bytes.Contains(stdout.Bytes(), []byte("SCREEN")) {
		t.Errorf("stdout = %q, want it to contain SCREEN", stdout.Bytes())
	}
}

// blockAfter wraps r so that once its bytes are exhausted, Read blocks instead
// of returning io.EOF — mimicking a real terminal stdin that never closes, so
// the input pump doesn't race the server into an early close.
func blockAfter(r io.Reader) io.Reader { return &blockingReader{r: r} }

type blockingReader struct {
	mu   sync.Mutex
	r    io.Reader
	done bool
}

func (b *blockingReader) Read(p []byte) (int, error) {
	b.mu.Lock()
	if !b.done {
		n, err := b.r.Read(p)
		if err == io.EOF {
			b.done = true
			b.mu.Unlock()
			select {} // block forever, like a live tty with no more input
		}
		b.mu.Unlock()
		return n, err
	}
	b.mu.Unlock()
	select {} // unreachable in practice
}
