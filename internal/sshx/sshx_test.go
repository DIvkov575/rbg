package sshx

import (
	"reflect"
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

func cfg() *config.Config {
	return &config.Config{Host: "desk", CWD: "", SSHOpts: nil, AgentPath: "rbg-agent"}
}

func TestQuoteToken(t *testing.T) {
	cases := map[string]string{
		"foo":           "'foo'",
		"a b":           "'a b'",
		"foo; rm -rf ~": "'foo; rm -rf ~'",
		"it's":          `'it'\''s'`,
		"":              "''",
	}
	for in, want := range cases {
		if got := QuoteToken(in); got != want {
			t.Errorf("QuoteToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoteCommand(t *testing.T) {
	got := RemoteCommand([]string{"rbg-agent", "send", "--task", "a; b"})
	want := "'rbg-agent' 'send' '--task' 'a; b'"
	if got != want {
		t.Errorf("RemoteCommand = %q, want %q", got, want)
	}
}

func TestBuildSSHArgs_Basic(t *testing.T) {
	got := BuildSSHArgs(cfg(), []string{"rbg-agent", "ls"}, Options{})
	want := []string{"desk", RemoteCommand([]string{"rbg-agent", "ls"})}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestBuildSSHArgs_OptsTTYBatch(t *testing.T) {
	c := cfg()
	c.SSHOpts = []string{"-p", "2222"}
	got := BuildSSHArgs(c, []string{"claude", "--resume", "x"}, Options{TTY: true})
	want := []string{"-t", "-p", "2222", "desk", RemoteCommand([]string{"claude", "--resume", "x"})}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	gotB := BuildSSHArgs(cfg(), []string{"true"}, Options{Batch: true})
	wantB := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "desk", "'true'"}
	if !reflect.DeepEqual(gotB, wantB) {
		t.Errorf("got %v want %v", gotB, wantB)
	}
}

func TestReachable(t *testing.T) {
	up := &run.Recording{Default: run.Result{Code: 0}}
	if !Reachable(cfg(), up) {
		t.Error("expected reachable when ssh true returns 0")
	}
	down := &run.Recording{Default: run.Result{Code: 255}}
	if Reachable(cfg(), down) {
		t.Error("expected unreachable when ssh returns 255")
	}
}

func TestAgentArgs_PrefixesCDWhenCWDSet(t *testing.T) {
	c := cfg()
	c.CWD = "/proj"
	// AgentArgs returns the remote argv (no shell). cd is passed as agent flag,
	// not a shell 'cd &&', so it stays structured.
	got := AgentArgs(c, "launch", []string{"--name", "x", "--task", "hi"})
	want := []string{"rbg-agent", "--cwd", "/proj", "launch", "--name", "x", "--task", "hi"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestAgentArgs_NoCDWhenEmpty(t *testing.T) {
	got := AgentArgs(cfg(), "ls", nil)
	want := []string{"rbg-agent", "ls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}
