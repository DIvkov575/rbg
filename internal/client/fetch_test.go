package client

import (
	"bytes"
	"strings"
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

func TestKill_InvokesAgentKill(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{"kill": {Stdout: []byte(`{"ok":"killed","id":"alpha"}`)}},
		Default:     run.Result{Code: 0},
	}
	var out bytes.Buffer
	if code := Kill(cfg(), r, &out, "alpha"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	joined := strings.Join(r.Calls[len(r.Calls)-1].Args, " ")
	if !strings.Contains(joined, "kill") || !strings.Contains(joined, "alpha") {
		t.Fatalf("kill call = %q", joined)
	}
}

func TestFetchDirs_ParsesListing(t *testing.T) {
	canned := `{"dir":"/home/u/proj","parent":"/home/u","entries":[{"name":"alpha","path":"/home/u/proj/alpha"},{"name":"beta","path":"/home/u/proj/beta"}]}`
	r := &run.Recording{
		BySubstring: map[string]run.Result{"lsdir": {Stdout: []byte(canned)}},
		Default:     run.Result{Code: 0},
	}
	listing, err := FetchDirs(cfg(), r, "/home/u/proj")
	if err != nil {
		t.Fatal(err)
	}
	if listing.Dir != "/home/u/proj" || listing.Parent != "/home/u" {
		t.Fatalf("listing = %+v", listing)
	}
	if len(listing.Entries) != 2 || listing.Entries[0].Name != "alpha" || listing.Entries[1].Path != "/home/u/proj/beta" {
		t.Fatalf("entries = %+v", listing.Entries)
	}
	joined := strings.Join(r.Calls[len(r.Calls)-1].Args, " ")
	if !strings.Contains(joined, "lsdir") || !strings.Contains(joined, "/home/u/proj") {
		t.Fatalf("lsdir call = %q", joined)
	}
}

func TestFetchDirs_UnreachableErrors(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 255}}
	if _, err := FetchDirs(cfg(), r, ""); err == nil {
		t.Fatal("expected error when ssh unreachable")
	}
}

func TestMakeDir_ParsesDir(t *testing.T) {
	canned := `{"dir":"/home/u/proj/new"}`
	r := &run.Recording{
		BySubstring: map[string]run.Result{"mkdir": {Stdout: []byte(canned)}},
		Default:     run.Result{Code: 0},
	}
	dir, err := MakeDir(cfg(), r, "/home/u/proj/new")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/home/u/proj/new" {
		t.Fatalf("dir = %q", dir)
	}
	joined := strings.Join(r.Calls[len(r.Calls)-1].Args, " ")
	if !strings.Contains(joined, "mkdir") || !strings.Contains(joined, "/home/u/proj/new") {
		t.Fatalf("mkdir call = %q", joined)
	}
}

func TestMakeDir_UnreachableErrors(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 255}}
	if _, err := MakeDir(cfg(), r, "/home/u/proj/new"); err == nil {
		t.Fatal("expected error when ssh unreachable")
	}
}

func TestCloneRepo_ParsesDir(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{"clone": {Stdout: []byte(`{"dir":"/home/u/rbg-repos/my-svc"}`)}},
		Default:     run.Result{Code: 0},
	}
	dir, err := CloneRepo(cfg(), r, "https://github.com/me/my-svc.git")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/home/u/rbg-repos/my-svc" {
		t.Fatalf("dir = %q", dir)
	}
}

func TestCloneRepo_ErrorJSON(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{"clone": {Stdout: []byte(`{"error":"git clone failed"}`), Code: 1}},
		Default:     run.Result{Code: 0},
	}
	if _, err := CloneRepo(cfg(), r, "bad"); err == nil {
		t.Fatal("expected error")
	}
}
