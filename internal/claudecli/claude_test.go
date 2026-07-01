package claudecli

import (
	"reflect"
	"testing"
)

func TestLaunchHeadlessArgs(t *testing.T) {
	got := LaunchHeadlessArgs("sid-x", "do it")
	want := []string{"-p", "do it", "--session-id", "sid-x", "--dangerously-skip-permissions"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResumeHeadlessArgs(t *testing.T) {
	got := ResumeHeadlessArgs("sid-1", "next step")
	want := []string{"-p", "next step", "--resume", "sid-1", "--dangerously-skip-permissions"}
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
