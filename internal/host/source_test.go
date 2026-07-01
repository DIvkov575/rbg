package host

import (
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

// A realistic two-element `claude agents --json --all` payload (verified shape).
const agentsPayload = `[
  {"id":"55a63641","cwd":"/home/me/app","kind":"background","startedAt":1782395439347,
   "sessionId":"55a63641-2b5e-413e-bd07-00a74bbc1dfc","name":"analyze","state":"done"},
  {"pid":70515,"id":"48fd50b3","cwd":"/home/me/svc","kind":"background","startedAt":1782840532214,
   "sessionId":"48fd50b3-9f01-4320-93ba-290d1c7c65a3","name":"init","status":"busy","state":"working"}
]`

func TestLocalSourceListParses(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte(agentsPayload), Code: 0}}
	live, err := LocalSource{R: r}.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("got %d live agents, want 2", len(live))
	}
	if live[0].SessionID != "55a63641-2b5e-413e-bd07-00a74bbc1dfc" || live[1].State != "working" {
		t.Errorf("parsed wrong: %+v", live)
	}
}

func TestLocalSourceExecsClaudeAgents(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte("[]"), Code: 0}}
	if _, err := (LocalSource{R: r}).List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(r.Calls) != 1 {
		t.Fatalf("made %d calls, want 1", len(r.Calls))
	}
	c := r.Calls[0]
	if c.Name != "claude" {
		t.Errorf("ran %q, want claude", c.Name)
	}
	// argv must be exactly the agents-list contract.
	want := []string{"agents", "--json", "--all"}
	if len(c.Args) != len(want) {
		t.Fatalf("args = %v, want %v", c.Args, want)
	}
	for i := range want {
		if c.Args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, c.Args[i], want[i])
		}
	}
}

func TestLocalSourceNonZeroExitErrors(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte("boom"), Code: 1}}
	if _, err := (LocalSource{R: r}).List(); err == nil {
		t.Errorf("expected error on non-zero exit, got nil")
	}
}

func TestLocalSourceBadJSONErrors(t *testing.T) {
	r := &run.Recording{Default: run.Result{Stdout: []byte("{not an array"), Code: 0}}
	if _, err := (LocalSource{R: r}).List(); err == nil {
		t.Errorf("expected error on unparseable JSON, got nil")
	}
}

func TestLocalSourceEmptyOutputIsEmptyList(t *testing.T) {
	for _, out := range []string{"", "   ", "\n\t "} {
		r := &run.Recording{Default: run.Result{Stdout: []byte(out), Code: 0}}
		live, err := LocalSource{R: r}.List()
		if err != nil {
			t.Errorf("empty output %q: unexpected error %v", out, err)
		}
		if len(live) != 0 {
			t.Errorf("empty output %q: got %d agents, want 0", out, len(live))
		}
	}
}

func TestRemoteSourceRunsClaudeOverSSH(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	r := &run.Recording{Default: run.Result{Stdout: []byte(agentsPayload), Code: 0}}
	live, err := RemoteSource{C: cfg, R: r}.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("got %d live agents, want 2", len(live))
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("expected one ssh call, got %+v", r.Calls)
	}
	// The remote command is the last ssh arg (a single shell-quoted string);
	// it must carry the claude agents-list invocation and target the host.
	args := r.Calls[0].Args
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	if !contains(joined, "desktop") {
		t.Errorf("ssh args missing host: %v", args)
	}
	if !contains(joined, "agents") || !contains(joined, "--json") {
		t.Errorf("ssh args missing claude agents invocation: %v", args)
	}
}

// contains is a tiny substring helper (avoids importing strings in the test).
func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return needle == ""
}
