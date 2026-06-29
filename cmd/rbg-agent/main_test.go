package main

import (
	"testing"
)

func TestParseFlags_LaunchWithCWD(t *testing.T) {
	inv, err := parseArgs([]string{"--cwd", "/proj", "launch", "--name", "x", "--task", "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.CWD != "/proj" || inv.Verb != "launch" || inv.Name != "x" || inv.Task != "hi" {
		t.Fatalf("inv = %+v", inv)
	}
}

func TestParseFlags_LsNoFlags(t *testing.T) {
	inv, err := parseArgs([]string{"ls"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Verb != "ls" {
		t.Fatalf("verb = %q", inv.Verb)
	}
}

func TestParseFlags_SendRequiresIDAndTask(t *testing.T) {
	inv, err := parseArgs([]string{"send", "--id", "alpha", "--task", "go"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Verb != "send" || inv.Name != "alpha" || inv.Task != "go" {
		t.Fatalf("inv = %+v", inv)
	}
}

func TestParseFlags_UnknownVerb(t *testing.T) {
	if _, err := parseArgs([]string{"frobnicate"}); err == nil {
		t.Fatal("expected error for unknown verb")
	}
}
