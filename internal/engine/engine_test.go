package engine

import (
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/host"
	"github.com/divkov575/rbg/internal/run"
)

// --- shared test fakes (used across engine_test.go and ops_test.go) ---

// fakeSource is a canned host.AgentSource.
type fakeSource struct {
	live []core.Live
	err  error
}

func (f fakeSource) List() ([]core.Live, error) { return f.live, f.err }

// fakeTx is a canned host.Transcripts that records the session it was asked for.
type fakeTx struct {
	data       []byte
	err        error
	gotSession *string // if non-nil, Read stores the requested session here
}

func (f fakeTx) Read(session string) ([]byte, error) {
	if f.gotSession != nil {
		*f.gotSession = session
	}
	return f.data, f.err
}

func TestNewWiresRealHostImpls(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	store := filepath.Join(t.TempDir(), "agents.json")
	e, err := New(cfg, run.Exec{}, store, "/home/me")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// local bundle must be the local impls; remote must be the remote impls.
	if _, ok := e.local.Source.(host.LocalSource); !ok {
		t.Errorf("local.Source = %T, want host.LocalSource", e.local.Source)
	}
	if _, ok := e.local.Tx.(host.LocalTranscripts); !ok {
		t.Errorf("local.Tx = %T, want host.LocalTranscripts", e.local.Tx)
	}
	if _, ok := e.remote.Source.(host.RemoteSource); !ok {
		t.Errorf("remote.Source = %T, want host.RemoteSource", e.remote.Source)
	}
	if _, ok := e.remote.Tx.(host.RemoteTranscripts); !ok {
		t.Errorf("remote.Tx = %T, want host.RemoteTranscripts", e.remote.Tx)
	}
}

func TestPickSelectsByLocation(t *testing.T) {
	// Distinct sentinel data per machine's Tx lets us prove pick returns the
	// right bundle.
	e := &Engine{
		local:  machine{Tx: fakeTx{data: []byte("LOCAL")}},
		remote: machine{Tx: fakeTx{data: []byte("REMOTE")}},
	}
	l, _ := e.pick(core.Local).Tx.Read("x")
	if string(l) != "LOCAL" {
		t.Errorf("pick(Local).Tx read %q, want LOCAL", l)
	}
	r, _ := e.pick(core.Remote).Tx.Read("x")
	if string(r) != "REMOTE" {
		t.Errorf("pick(Remote).Tx read %q, want REMOTE", r)
	}
}
