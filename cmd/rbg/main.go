// Command rbg is the laptop client. It resolves config, runs the connection
// gate, and invokes rbg-agent on the desktop over ssh.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		return &inv{verb: "dash"}, nil // no args → dashboard
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		return &inv{verb: "help"}, nil
	}
	in := &inv{verb: args[0]}
	rest := args[1:]
	switch in.verb {
	case "ls", "ping", "deploy", "dash":
		return in, nil
	case "launch":
		switch len(rest) {
		case 1:
			in.task = rest[0] // name auto-derived by the agent
		case 2:
			in.name, in.task = rest[0], rest[1]
		default:
			return nil, fmt.Errorf("launch requires \"<task>\" or <name> \"<task>\"")
		}
	case "send":
		if len(rest) < 2 {
			return nil, fmt.Errorf("send requires <name> <task>")
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
	case "attach", "kill":
		if len(rest) < 1 {
			return nil, fmt.Errorf("%s requires <name>", in.verb)
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
		fmt.Fprintf(os.Stderr, "rbg: %v\n\n%s", err, usage())
		os.Exit(2)
	}
	if in.verb == "help" {
		fmt.Print(usage())
		os.Exit(0)
	}
	cfg, err := config.Load(envMap(), os.ExpandEnv("$HOME/.rbg.conf"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
		os.Exit(2)
	}
	if cfg.Mux {
		ensureControlDir(cfg.ControlPath)
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
	case "kill":
		os.Exit(client.Kill(cfg, r, os.Stdout, in.name))
	case "deploy":
		os.Exit(deploy(cfg, r))
	case "dash":
		os.Exit(dash(cfg, r))
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

// usage returns the full help text (verbs + config), printed by `rbg help`,
// `-h`/`--help`, and on a parse error.
func usage() string {
	return `rbg — manage remote Claude agents on a dev desktop over SSH

Usage:
  rbg [command] [args]

Commands:
  launch "<task>"           launch a new agent; name auto-derived from the task
  launch <name> "<task>"    launch a new agent with an explicit name
  send <name> "<task>"      send a follow-up task to an existing agent
  read <name> [-f]          print an agent's transcript (-f/--follow reserved)
  ls                        list agents recorded on the desktop
  attach <name>             attach to an agent interactively (TTY)
  kill <name>               forget an agent (terminate live child; keep transcript)
  ping                      check the desktop is reachable
  deploy                    build and install the agent binary on the desktop
  dash                      interactive dashboard (also the default with no args)
  help, -h, --help          show this help

Configuration (environment, or ~/.rbg.conf as KEY=value lines; env wins):
  RBG_HOST         desktop hostname (required)
  RBG_CWD          remote working directory for agents
  RBG_SSH          extra ssh options (e.g. "-i ~/.ssh/key -p 2222")
  RBG_AGENT_PATH   remote agent path (default: .local/bin/rbg-agent)

Examples:
  rbg deploy
  rbg launch "investigate the flaky payments test"
  rbg ls
  rbg send fix-flaky-test "now write the fix and run the tests"
  rbg read fix-flaky-test
`
}

// ensureControlDir creates the parent directory of the SSH ControlPath socket,
// expanding a leading ~/. ssh does not create it and fails to multiplex if it
// is missing. Best-effort: a failure here just means no socket reuse.
func ensureControlDir(controlPath string) {
	if strings.HasPrefix(controlPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			controlPath = filepath.Join(home, controlPath[2:])
		}
	}
	_ = os.MkdirAll(filepath.Dir(controlPath), 0o700)
}
