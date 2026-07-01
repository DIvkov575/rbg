package main

import "testing"

func TestIsScriptableClassifiesVerbs(t *testing.T) {
	for _, v := range []string{"ls", "create", "run", "send", "read", "kill", "adopt"} {
		if !isScriptable(v) {
			t.Errorf("verb %q should route to the scriptable CLI", v)
		}
	}
	for _, v := range []string{"dash", "deploy", "ping", "attach", "help", "frob"} {
		if isScriptable(v) {
			t.Errorf("verb %q should NOT be scriptable", v)
		}
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
