// Command rbg is the laptop client. It resolves config, runs the connection
// gate, and invokes rbg-agent on the desktop over ssh.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/divkov575/rbg/internal/cli"
	"github.com/divkov575/rbg/internal/client"
	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/engine"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 || args[0] == "dash" {
		os.Exit(runDash())
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(cli.Usage())
		os.Exit(0)
	}
	// `raw` prefix accepted and stripped (back-compat).
	if args[0] == "raw" {
		args = args[1:]
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "rbg: raw requires a verb")
			os.Exit(2)
		}
	}
	verb := args[0]
	switch verb {
	case "deploy", "ping", "attach":
		os.Exit(runLegacy(verb, args[1:]))
	}
	if isScriptable(verb) {
		e, err := buildEngine()
		if err != nil {
			fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
			os.Exit(2)
		}
		os.Exit(cli.Dispatch(args, e, os.Stdout))
	}
	fmt.Fprintf(os.Stderr, "rbg: unknown command %q\n\n%s", verb, cli.Usage())
	os.Exit(2)
}

// isScriptable reports whether a verb is handled by the engine-backed CLI.
func isScriptable(verb string) bool {
	switch verb {
	case "ls", "create", "run", "send", "read", "kill", "adopt":
		return true
	}
	return false
}

// buildEngine loads config and constructs a real Engine over ssh/exec.
func buildEngine() (*engine.Engine, error) {
	cfg, err := loadCfg()
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	storePath := filepath.Join(home, ".rbg", "agents.json")
	return engine.New(cfg, run.Exec{}, storePath, home)
}

// loadCfg loads config and ensures the ssh control dir when muxing.
func loadCfg() (*config.Config, error) {
	cfg, err := config.Load(envMap(), os.ExpandEnv("$HOME/.rbg.conf"))
	if err != nil {
		return nil, err
	}
	if cfg.Mux {
		ensureControlDir(cfg.ControlPath)
	}
	return cfg, nil
}

// runDash loads config and opens the interactive dashboard (unchanged path).
func runDash() int {
	cfg, err := loadCfg()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
		return 2
	}
	return dash(cfg, run.Exec{})
}

// runLegacy dispatches the non-engine verbs (deploy/ping/attach) to their
// existing handlers.
func runLegacy(verb string, rest []string) int {
	cfg, err := loadCfg()
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
		return 2
	}
	r := run.Exec{}
	switch verb {
	case "deploy":
		return deploy(cfg, r)
	case "ping":
		return client.Ping(cfg, r, os.Stdout)
	case "attach":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "rbg: attach requires <name>")
			return 2
		}
		return attach(cfg, r, rest[0])
	}
	return 2
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
