package main

import "testing"

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
