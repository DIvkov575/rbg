package localagent

import (
	"reflect"
	"testing"
)

type recExec struct{ calls [][]string }

func (r *recExec) Run(name string, args []string, dir string) error {
	r.calls = append(r.calls, append([]string{name + "@" + dir}, args...))
	return nil
}

func TestPlanRun_PullsWhenCloneExists(t *testing.T) {
	p := PlanRun(Agent{Repo: "/x/svc"}, "do it", func(string) bool { return true })
	if p.Clone {
		t.Fatal("should not clone when .git present")
	}
	if !reflect.DeepEqual(p.Steps[0], []string{"git", "-C", "/x/svc", "pull", "--ff-only"}) {
		t.Fatalf("first step should be a pull: %v", p.Steps[0])
	}
	last := p.Steps[len(p.Steps)-1]
	if last[0] != "claude" || last[2] != "do it" {
		t.Fatalf("last step should run claude with the task: %v", last)
	}
}

func TestPlanRun_ClonesWhenAbsent(t *testing.T) {
	p := PlanRun(Agent{Repo: "https://github.com/me/svc.git"}, "t", func(string) bool { return false })
	if !p.Clone || p.Steps[0][1] != "clone" {
		t.Fatalf("should clone when absent: %+v", p)
	}
}

func TestRun_BlankAgentErrors(t *testing.T) {
	if _, err := Run(&recExec{}, Agent{Name: "b", Repo: "r"}, ""); err == nil {
		t.Fatal("blank agent with no task should error")
	}
}

func TestRun_TaskOverridesDefault(t *testing.T) {
	ex := &recExec{}
	a := Agent{Name: "a", Repo: "/x/svc", Task: "default task"}
	if _, err := Run(ex, a, "override"); err != nil {
		t.Fatal(err)
	}
	// last call is claude with the override task, run in the repo dir
	last := ex.calls[len(ex.calls)-1]
	if last[0] != "claude@/x/svc" || last[2] != "override" {
		t.Fatalf("claude call = %v", last)
	}
}

func TestRun_UsesDefaultTaskWhenNoOverride(t *testing.T) {
	ex := &recExec{}
	if _, err := Run(ex, Agent{Name: "a", Repo: "/x/svc", Task: "the default"}, ""); err != nil {
		t.Fatal(err)
	}
	last := ex.calls[len(ex.calls)-1]
	if last[2] != "the default" {
		t.Fatalf("should use default task: %v", last)
	}
}
