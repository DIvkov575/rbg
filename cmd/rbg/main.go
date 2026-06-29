// Command rbg is the laptop client. It resolves config, runs the connection
// gate, and invokes rbg-agent on the desktop over ssh.
package main

import (
	"fmt"
	"os"

	"github.com/divkov575/rbg/internal/client"
	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

type inv struct {
	verb   string
	name   string
	task   string
	follow bool
}

func parse(args []string) (*inv, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: rbg <launch|send|read|ls|attach|ping|deploy> ...")
	}
	in := &inv{verb: args[0]}
	rest := args[1:]
	switch in.verb {
	case "ls", "ping", "deploy":
		return in, nil
	case "launch", "send":
		if len(rest) < 2 {
			return nil, fmt.Errorf("%s requires <name> <task>", in.verb)
		}
		in.name, in.task = rest[0], rest[1]
	case "read":
		if len(rest) < 1 {
			return nil, fmt.Errorf("read requires <name>")
		}
		in.name = rest[0]
		for _, a := range rest[1:] {
			if a == "-f" || a == "--follow" {
				in.follow = true
			}
		}
	case "attach":
		if len(rest) < 1 {
			return nil, fmt.Errorf("attach requires <name>")
		}
		in.name = rest[0]
	default:
		return nil, fmt.Errorf("unknown verb %q", in.verb)
	}
	return in, nil
}

func main() {
	in, err := parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
		os.Exit(2)
	}
	cfg, err := config.Load(envMap(), os.ExpandEnv("$HOME/.rbg.conf"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
		os.Exit(2)
	}
	r := run.Exec{}
	switch in.verb {
	case "ping":
		os.Exit(client.Ping(cfg, r, os.Stdout))
	case "launch":
		os.Exit(client.Launch(cfg, r, os.Stdout, in.name, in.task))
	case "send":
		os.Exit(client.Send(cfg, r, os.Stdout, in.name, in.task))
	case "read":
		os.Exit(client.Read(cfg, r, os.Stdout, in.name))
	case "ls":
		os.Exit(client.Ls(cfg, r, os.Stdout))
	case "attach":
		os.Exit(attach(cfg, r, in.name))
	case "deploy":
		os.Exit(deploy(cfg, r))
	}
}

// attach resolves the claude session id from the agent's ls, then drops into an
// interactive `claude --resume` over an ssh tty.
func attach(cfg *config.Config, r run.Runner, name string) int {
	sshx.EnsureReachable(cfg, r)
	// For attach we shell out to ssh -t directly so the user gets the real tty;
	// we pass claude --resume with the recorded id. Resolve id via agent ls.
	body, code := func() ([]byte, int) {
		out, c, _ := r.Run("ssh", sshx.BuildSSHArgs(cfg, sshx.AgentArgs(cfg, "ls", nil), sshx.Options{}), nil)
		return out, c
	}()
	if code != 0 {
		fmt.Fprintf(os.Stderr, "rbg: could not list sessions for attach\n")
		return code
	}
	id := claudeSessionIDFor(body, name)
	if id == "" {
		fmt.Fprintf(os.Stderr, "rbg: unknown agent %q\n", name)
		return 1
	}
	args := sshx.BuildSSHArgs(cfg, []string{"claude", "--resume", id}, sshx.Options{TTY: true})
	// Interactive: connect to the real terminal.
	return runInteractive("ssh", args)
}
