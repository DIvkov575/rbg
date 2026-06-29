package client

import (
	"testing"

	"github.com/divkov575/rbg/internal/run"
)

func TestFetchSessions_ParsesLsJSON(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"ls": {Stdout: []byte(`[{"name":"alpha","claudeSessionId":"sid-1","transcriptPath":"/t/a"},{"name":"beta","claudeSessionId":"sid-2"}]`)},
		},
		Default: run.Result{Code: 0},
	}
	sessions, err := FetchSessions(cfg(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].Name != "alpha" || sessions[1].ClaudeSessionID != "sid-2" {
		t.Fatalf("sessions = %+v", sessions)
	}
}

func TestFetchSessions_NonZeroExitErrors(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{"ls": {Code: 1, Stdout: []byte("boom")}},
		Default:     run.Result{Code: 0}, // gate reachable; only ls fails
	}
	if _, err := FetchSessions(cfg(), r); err == nil {
		t.Fatal("expected error on nonzero exit")
	}
}

func TestFetchTranscript_ReturnsRenderedText(t *testing.T) {
	// The agent's `read` already renders the JSONL transcript, so the stub
	// returns the rendered text and FetchTranscript passes it through verbatim.
	rendered := "user: q\nassistant: a\n"
	r := &run.Recording{
		BySubstring: map[string]run.Result{"read": {Stdout: []byte(rendered)}},
		Default:     run.Result{Code: 0},
	}
	text, err := FetchTranscript(cfg(), r, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if text != "user: q\nassistant: a\n" {
		t.Fatalf("text = %q", text)
	}
}
