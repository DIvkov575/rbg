package claudecli

import (
	"reflect"
	"testing"
)

func TestBGArgs(t *testing.T) {
	got := BGArgs("alpha", "do the thing")
	want := []string{"--bg", "-n", "alpha", "do the thing"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResumeHeadlessArgs(t *testing.T) {
	got := ResumeHeadlessArgs("sid-1", "next step")
	want := []string{"-p", "next step", "--resume", "sid-1", "--output-format", "stream-json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestAgentsListArgs(t *testing.T) {
	got := AgentsListArgs()
	want := []string{"agents", "--json", "--all"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestParseAgents_BareArrayAndWrapped(t *testing.T) {
	a, err := ParseAgents([]byte(`[{"name":"a","sessionId":"x"}]`))
	if err != nil || len(a) != 1 || a[0].Name != "a" || a[0].SessionID != "x" {
		t.Fatalf("bare array parse: %+v err=%v", a, err)
	}
	b, err := ParseAgents([]byte(`{"agents":[{"name":"b","session_id":"y"}]}`))
	if err != nil || len(b) != 1 || b[0].SessionID != "y" {
		t.Fatalf("wrapped parse: %+v err=%v", b, err)
	}
}

func TestParseAgents_Garbage(t *testing.T) {
	a, _ := ParseAgents([]byte("not json"))
	if len(a) != 0 {
		t.Fatalf("garbage should yield empty, got %+v", a)
	}
}

func TestFindSessionID(t *testing.T) {
	agents := []Agent{{Name: "alpha", SessionID: "sid-a"}, {Name: "beta", SessionID: "sid-b"}}
	if got := FindSessionID(agents, "beta"); got != "sid-b" {
		t.Errorf("got %q want sid-b", got)
	}
	if got := FindSessionID(agents, "ghost"); got != "" {
		t.Errorf("ghost should be empty, got %q", got)
	}
}
