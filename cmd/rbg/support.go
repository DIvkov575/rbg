package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// envMap snapshots the process environment into a map for config.Load.
func envMap() map[string]string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// claudeSessionIDFor extracts the claudeSessionId for name from the agent's ls
// JSON array.
func claudeSessionIDFor(lsJSON []byte, name string) string {
	var list []struct {
		Name            string `json:"name"`
		ClaudeSessionID string `json:"claudeSessionId"`
	}
	if err := json.Unmarshal(lsJSON, &list); err != nil {
		return ""
	}
	for _, s := range list {
		if s.Name == name {
			return s.ClaudeSessionID
		}
	}
	return ""
}

// runInteractive runs ssh with the real stdio so the user gets an interactive
// tty (used by attach).
func runInteractive(name string, args []string) int {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// deploy cross-compiles rbg-agent for the desktop's arch and scp's it into
// place. The desktop arch is probed via `uname -m` over ssh.
func deploy(cfg *config.Config, r run.Runner) int {
	sshx.EnsureReachable(cfg, r)
	unameOut, code, _ := r.Run("ssh", sshx.BuildSSHArgs(cfg, []string{"uname", "-m"}, sshx.Options{}), nil)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "rbg: could not probe desktop arch\n")
		return 1
	}
	goarch := archFromUname(strings.TrimSpace(string(unameOut)))
	if goarch == "" {
		fmt.Fprintf(os.Stderr, "rbg: unsupported desktop arch %q\n", strings.TrimSpace(string(unameOut)))
		return 1
	}
	tmp, err := os.MkdirTemp("", "rbg-agent-build")
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg: %v\n", err)
		return 1
	}
	out := tmp + "/rbg-agent"
	build := exec.Command("go", "build", "-o", out, "github.com/divkov575/rbg/cmd/rbg-agent")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+goarch, "CGO_ENABLED=0")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "rbg: agent build failed: %v\n", err)
		return 1
	}
	// scp into ~/.local/bin on the desktop (strip any leading ~ for scp dest).
	dest := cfg.Host + ":.local/bin/rbg-agent"
	mkdir := sshx.BuildSSHArgs(cfg, []string{"mkdir", "-p", ".local/bin"}, sshx.Options{})
	if _, c, _ := r.Run("ssh", mkdir, nil); c != 0 {
		fmt.Fprintf(os.Stderr, "rbg: could not create remote bin dir\n")
		return 1
	}
	scpArgs := append([]string{}, cfg.SSHOpts...)
	scpArgs = append(scpArgs, out, dest)
	if _, c, _ := r.Run("scp", scpArgs, nil); c != 0 {
		fmt.Fprintf(os.Stderr, "rbg: scp failed\n")
		return 1
	}
	fmt.Printf("deployed rbg-agent (%s/%s) to %s\n", "linux", goarch, dest)
	return 0
}

func archFromUname(m string) string {
	switch m {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return ""
}
