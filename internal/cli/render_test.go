package cli

import (
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

func TestRenderAgents(t *testing.T) {
	agents := []core.Agent{
		{Name: "fix-bug", Repo: "app", Where: core.Remote, State: core.Running, Origin: core.Managed},
		{Name: "held-one", Repo: "app", Where: core.Local, State: core.Held, Origin: core.Managed},
		{Name: "wild", Where: core.Remote, State: core.Done, Origin: core.Foreign},
	}
	out := renderAgents(agents)
	for _, n := range []string{"fix-bug", "held-one", "wild"} {
		if !strings.Contains(out, n) {
			t.Errorf("render missing agent %q:\n%s", n, out)
		}
	}
	if !strings.Contains(out, "running") || !strings.Contains(out, "remote") {
		t.Errorf("render should show state+location:\n%s", out)
	}
	if !strings.Contains(out, "foreign") {
		t.Errorf("render should mark foreign agents:\n%s", out)
	}
}

func TestRenderAgentsEmpty(t *testing.T) {
	out := renderAgents(nil)
	if !strings.Contains(strings.ToLower(out), "no agents") {
		t.Errorf("empty render should say so: %q", out)
	}
}
