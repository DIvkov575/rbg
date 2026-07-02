package ptybridge

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestDataFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello \x1b[0m world\x00")
	if err := WriteData(&buf, payload); err != nil {
		t.Fatalf("WriteData: %v", err)
	}
	kind, got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if kind != KindData {
		t.Errorf("kind = %d, want %d", kind, KindData)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("payload = %q, want %q", got, payload)
	}
}

func TestCtrlFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCtrl(&buf, Ctrl{T: "resize", Cols: 120, Rows: 40}); err != nil {
		t.Fatalf("WriteCtrl: %v", err)
	}
	kind, payload, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if kind != KindCtrl {
		t.Fatalf("kind = %d, want %d", kind, KindCtrl)
	}
	c, err := decodeCtrl(payload)
	if err != nil {
		t.Fatalf("decodeCtrl: %v", err)
	}
	if c.T != "resize" || c.Cols != 120 || c.Rows != 40 {
		t.Errorf("ctrl = %+v, want resize/120/40", c)
	}
}

// TestWireMatchesObservedHello verifies our reader parses the exact framing a
// live worker emits: [4-byte BE payload length][kind byte 0x01][JSON body].
// Confirmed on the wire: a 48-byte JSON hello payload has length prefix 48.
func TestWireMatchesObservedHello(t *testing.T) {
	body := []byte(`{"t":"hello","replPid":8644,"version":"2.1.197"}`)
	frame := make([]byte, 5+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body)))
	frame[4] = KindCtrl
	copy(frame[5:], body)

	kind, payload, err := ReadFrame(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if kind != KindCtrl {
		t.Fatalf("kind = %d, want ctrl", kind)
	}
	c, err := decodeCtrl(payload)
	if err != nil {
		t.Fatalf("decodeCtrl: %v", err)
	}
	if c.T != "hello" || c.ReplPid != 8644 {
		t.Errorf("ctrl = %+v, want hello/8644", c)
	}
}

func TestReadFrameRejectsOversizeLength(t *testing.T) {
	hdr := make([]byte, 5)
	binary.BigEndian.PutUint32(hdr[:4], maxFrame+1)
	hdr[4] = KindData
	_, _, err := ReadFrame(bytes.NewReader(hdr))
	if err == nil {
		t.Fatal("expected error on oversize frame length")
	}
}

func TestReadFrameEOFBetweenFrames(t *testing.T) {
	_, _, err := ReadFrame(bytes.NewReader(nil))
	if err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
}
