package main

import (
	"strings"
	"testing"
)

func TestParse_Launch(t *testing.T) {
	inv, err := parse([]string{"launch", "alpha", "do the thing"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.verb != "launch" || inv.name != "alpha" || inv.task != "do the thing" {
		t.Fatalf("inv = %+v", inv)
	}
}

func TestParse_ReadFollow(t *testing.T) {
	inv, err := parse([]string{"read", "alpha", "-f"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.verb != "read" || inv.name != "alpha" || !inv.follow {
		t.Fatalf("inv = %+v", inv)
	}
}

func TestParse_LsPing(t *testing.T) {
	for _, v := range []string{"ls", "ping"} {
		inv, err := parse([]string{v})
		if err != nil || inv.verb != v {
			t.Fatalf("verb %q: inv=%+v err=%v", v, inv, err)
		}
	}
}

func TestParse_UnknownVerb(t *testing.T) {
	if _, err := parse([]string{"frob"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParse_AttachAndDeploy(t *testing.T) {
	if inv, err := parse([]string{"attach", "alpha"}); err != nil || inv.verb != "attach" || inv.name != "alpha" {
		t.Fatalf("attach: %+v %v", inv, err)
	}
	if inv, err := parse([]string{"deploy"}); err != nil || inv.verb != "deploy" {
		t.Fatalf("deploy: %+v %v", inv, err)
	}
}

func TestClaudeSessionIDFor(t *testing.T) {
	ls := []byte(`[{"name":"alpha","claudeSessionId":"sid-a"},{"name":"beta","claudeSessionId":"sid-b"}]`)
	if got := claudeSessionIDFor(ls, "beta"); got != "sid-b" {
		t.Errorf("got %q want sid-b", got)
	}
	if got := claudeSessionIDFor(ls, "ghost"); got != "" {
		t.Errorf("ghost should be empty, got %q", got)
	}
}

func TestParse_LaunchTaskOnly(t *testing.T) {
	in, err := parse([]string{"launch", "do the thing"})
	if err != nil {
		t.Fatal(err)
	}
	if in.verb != "launch" || in.name != "" || in.task != "do the thing" {
		t.Fatalf("inv = %+v", in)
	}
}

func TestParse_Dash(t *testing.T) {
	in, err := parse([]string{"dash"})
	if err != nil || in.verb != "dash" {
		t.Fatalf("dash: inv=%+v err=%v", in, err)
	}
}

func TestParse_NoArgsDefaultsToDash(t *testing.T) {
	in, err := parse([]string{})
	if err != nil || in.verb != "dash" {
		t.Fatalf("no-args: inv=%+v err=%v", in, err)
	}
}

func TestParse_HelpVariants(t *testing.T) {
	for _, a := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		in, err := parse(a)
		if err != nil {
			t.Fatalf("parse(%v) err=%v", a, err)
		}
		if in.verb != "help" {
			t.Fatalf("parse(%v) verb=%q, want help", a, in.verb)
		}
	}
}

func TestUsageMentionsEveryVerb(t *testing.T) {
	u := usage()
	for _, v := range []string{"launch", "send", "read", "ls", "attach", "ping", "deploy", "dash", "help"} {
		if !strings.Contains(u, v) {
			t.Errorf("usage() missing verb %q", v)
		}
	}
	for _, e := range []string{"RBG_HOST", "RBG_CWD", "RBG_SSH", "RBG_AGENT_PATH"} {
		if !strings.Contains(u, e) {
			t.Errorf("usage() missing config var %q", e)
		}
	}
}

func TestParse_RawPrefix(t *testing.T) {
	// `rbg raw ls` parses as the ls verb
	in, err := parse([]string{"raw", "ls"})
	if err != nil || in.verb != "ls" {
		t.Fatalf("raw ls: inv=%+v err=%v", in, err)
	}
	// `rbg raw launch <name> <task>` keeps its args
	in, err = parse([]string{"raw", "launch", "alpha", "do it"})
	if err != nil || in.verb != "launch" || in.name != "alpha" || in.task != "do it" {
		t.Fatalf("raw launch: inv=%+v err=%v", in, err)
	}
}

func TestParse_RawRequiresVerb(t *testing.T) {
	if _, err := parse([]string{"raw"}); err == nil {
		t.Fatal("bare `raw` should error (needs a verb)")
	}
}

func TestParse_BareStillDash(t *testing.T) {
	in, err := parse([]string{})
	if err != nil || in.verb != "dash" {
		t.Fatalf("bare rbg should be dash, got %+v err=%v", in, err)
	}
}

func TestParse_Kill(t *testing.T) {
	in, err := parse([]string{"kill", "alpha"})
	if err != nil || in.verb != "kill" || in.name != "alpha" {
		t.Fatalf("kill: inv=%+v err=%v", in, err)
	}
	if _, err := parse([]string{"kill"}); err == nil {
		t.Fatal("kill without name should error")
	}
}

func TestLocalRepoDir(t *testing.T) {
	cases := map[string]string{
		"https://github.com/DIvkov575/mymemories.git": "/workplace/mymemories",
		"git@github.com:me/web-app.git":               "/workplace/web-app",
		"my-svc":                                      "/workplace/my-svc",
	}
	for in, suffix := range cases {
		got := localRepoDir(in)
		if len(got) < len(suffix) || got[len(got)-len(suffix):] != suffix {
			t.Errorf("localRepoDir(%q) = %q, want suffix %q", in, got, suffix)
		}
	}
}
