// Command rbg-agent runs on the desktop. sshd execs it directly with a
// structured argv; it never sees a shell.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/divkov575/rbg/internal/agent"
	"github.com/divkov575/rbg/internal/run"
)

// invocation is the parsed command line.
type invocation struct {
	CWD  string
	Verb string
	Name string // --name (launch) or --id (send/read)
	Task string
	Dir  string // --dir (lsdir; optional)
}

func parseArgs(args []string) (*invocation, error) {
	inv := &invocation{}
	// leading --cwd <dir> is global
	i := 0
	for i < len(args) && args[i] == "--cwd" {
		if i+1 >= len(args) {
			return nil, errors.New("--cwd requires a value")
		}
		inv.CWD = args[i+1]
		i += 2
	}
	if i >= len(args) {
		return nil, errors.New("missing verb")
	}
	inv.Verb = args[i]
	i++
	rest := args[i:]
	switch inv.Verb {
	case "ls", "ping", "version":
		return inv, nil
	case "launch":
		inv.Name = flagValue(rest, "--name") // optional now
		inv.Task = flagValue(rest, "--task")
		if inv.Task == "" {
			return nil, errors.New("launch requires --task")
		}
	case "send":
		inv.Name = flagValue(rest, "--id")
		inv.Task = flagValue(rest, "--task")
		if inv.Name == "" || inv.Task == "" {
			return nil, errors.New("send requires --id and --task")
		}
	case "read":
		inv.Name = flagValue(rest, "--id")
		if inv.Name == "" {
			return nil, errors.New("read requires --id")
		}
	case "lsdir":
		// --dir is optional; empty means the agent picks its default.
		inv.Dir = flagValue(rest, "--dir")
	case "mkdir":
		inv.Dir = flagValue(rest, "--dir")
		if inv.Dir == "" {
			return nil, errors.New("mkdir requires --dir")
		}
	case "kill":
		inv.Name = flagValue(rest, "--id")
		if inv.Name == "" {
			return nil, errors.New("kill requires --id")
		}
	default:
		return nil, fmt.Errorf("unknown verb %q", inv.Verb)
	}
	return inv, nil
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func newAgent(launchDir string) *agent.Agent {
	home, _ := os.UserHomeDir()
	return &agent.Agent{
		Runner:     run.Exec{},
		StatePath:  filepath.Join(home, ".rbg-agent", "sessions.json"),
		ClaudeHome: home,
		Now:        func() string { return time.Now().UTC().Format(time.RFC3339Nano) },
		LaunchDir:  launchDir,
	}
}

func main() {
	inv, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg-agent: %v\n", err)
		os.Exit(2)
	}
	a := newAgent(inv.CWD)
	switch inv.Verb {
	case "version":
		fmt.Println("rbg-agent v2")
		os.Exit(0)
	case "ping":
		fmt.Println("ok")
		os.Exit(0)
	case "launch":
		os.Exit(a.Launch(os.Stdout, inv.Name, inv.Task))
	case "send":
		os.Exit(a.Send(os.Stdout, inv.Name, inv.Task))
	case "read":
		os.Exit(a.Read(os.Stdout, inv.Name))
	case "ls":
		os.Exit(a.Ls(os.Stdout))
	case "lsdir":
		os.Exit(a.Lsdir(os.Stdout, inv.Dir))
	case "mkdir":
		os.Exit(a.Mkdir(os.Stdout, inv.Dir))
	case "kill":
		os.Exit(a.Kill(os.Stdout, inv.Name))
	}
}
