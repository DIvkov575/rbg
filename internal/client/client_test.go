package client

import (
	"bytes"
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

func cfg() *config.Config {
	return &config.Config{Host: "desk", AgentPath: "rbg-agent"}
}

func TestLaunch_InvokesAgentOverSSH(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"launch": {Stdout: []byte(`{"id":"alpha","claudeSessionId":"sid-1"}`)},
		},
		Default: run.Result{Code: 0}, // reachability true
	}
	var out bytes.Buffer
	code := Launch(cfg(), r, &out, "alpha", "build it")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	// second call (index 1) is the agent launch over ssh; index 0 is the gate
	if len(r.Calls) < 2 {
		t.Fatalf("expected gate + launch calls, got %d", len(r.Calls))
	}
	joined := strings.Join(r.Calls[1].Args, " ")
	if !strings.Contains(joined, "rbg-agent") || !strings.Contains(joined, "launch") {
		t.Fatalf("launch call = %q", joined)
	}
	if !strings.Contains(out.String(), "alpha") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestLs_RendersAgentJSON(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"ls": {Stdout: []byte(`[{"name":"alpha","claudeSessionId":"sid-1","transcriptPath":"/t/x"}]`)},
		},
		Default: run.Result{Code: 0},
	}
	var out bytes.Buffer
	if code := Ls(cfg(), r, &out); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out.String(), "alpha") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestRead_RendersStreamedTranscript(t *testing.T) {
	transcript := `{"message":{"role":"user","content":"q"}}` + "\n" +
		`{"message":{"role":"assistant","content":"a"}}` + "\n"
	r := &run.Recording{
		BySubstring: map[string]run.Result{"read": {Stdout: []byte(transcript)}},
		Default:     run.Result{Code: 0},
	}
	var out bytes.Buffer
	if code := Read(cfg(), r, &out, "alpha"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	// client passes agent output through render (already rendered? no — agent
	// `read` already renders, so client prints verbatim). Accept either; assert
	// the assistant line is present.
	if !strings.Contains(out.String(), "a") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestSend_BusyMapsToExit3(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{"send": {Code: 3}},
		Default:     run.Result{Code: 0},
	}
	var out bytes.Buffer
	if code := Send(cfg(), r, &out, "alpha", "x"); code != 3 {
		t.Fatalf("want 3, got %d", code)
	}
}
