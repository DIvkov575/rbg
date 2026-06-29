package slug

import "testing"

func TestFromTask(t *testing.T) {
	cases := map[string]string{
		"fix the flaky payments test": "fix-flaky-payments-test",
		"Refactor the Auth Module":     "refactor-auth-module",
		"investigate a bug in setup()": "investigate-bug-setup",
		"the a an to of":               "agent", // all stopwords → fallback
		"":                             "agent",
		"!!!  @@@":                     "agent", // no alnum → fallback
		"a very long task description that keeps going and going forever":
			"very-long-task-description", // capped at 4 words
	}
	for in, want := range cases {
		if got := FromTask(in); got != want {
			t.Errorf("FromTask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromTaskMaxLen(t *testing.T) {
	got := FromTask("supercalifragilistic expialidocious")
	if len(got) > 40 {
		t.Errorf("slug too long (%d): %q", len(got), got)
	}
}
