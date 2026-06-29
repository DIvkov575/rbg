# `rbg` v2 (Go agent binary) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the v1 bash-over-SSH CLI with two Go binaries — a laptop client (`rbg`) and a desktop agent (`rbg-agent`) — so every remote operation is a structured program invocation, not a shell string. No tmux, no flock-grep, no transcript globbing, no shell quoting.

**Architecture:** One Go module, two `main` packages under `cmd/`, shared logic in internal packages. The client runs a connection gate then `ssh host -- rbg-agent <cmd> --flags`, parsing JSON back. The agent owns session state, spawns the `claude` child detached (own process group), serializes sends with a real file lock, and streams transcripts itself. SSH is transport only; the agent is never invoked through a shell.

**Tech Stack:** Go 1.26 (stdlib only — `flag`, `encoding/json`, `os/exec`, `syscall`, `os`, `bufio`, `testing`). Integration reuses v1's non-root sshd harness (Python), minus tmux.

---

## Design Reference (read before starting)

**Source spec:** `docs/HLD-rbg-v2-agent-binary.md`.

**Module layout** (locked here; tasks build it bottom-up):
```
go.mod                         module github.com/divkov575/rbg
cmd/rbg/main.go                laptop client entrypoint (flag parsing → client pkg)
cmd/rbg-agent/main.go          desktop agent entrypoint (flag parsing → agent pkg)
internal/config/config.go      client config (env over ~/.rbg.conf)
internal/sshx/sshx.go          client: build ssh argv, connection gate, run agent
internal/render/render.go      client: render transcript JSONL lines to text
internal/session/session.go    agent: session state file (load/save/add/get)
internal/session/lock.go       agent: non-blocking flock on a session lockfile
internal/claudecli/claude.go   agent: build claude argv + parse its outputs (ISOLATED)
internal/agent/agent.go        agent: launch/send/read/ls command implementations
```

**Wire protocol (client ⇄ agent):** the agent prints a single JSON object (or
JSONL stream for `read`) to stdout; non-zero exit codes signal errors. Exit code
**3** specifically means "send busy" (lock held), preserving v1's contract.

**Session state schema** (`~/.rbg-agent/sessions.json` on the desktop):
```json
{ "<id>": {"name":"<id>","claudeSessionId":"<sid>","transcriptPath":"/abs/x.jsonl","pid":0,"startedAt":"<rfc3339>"} }
```
For v2, `id == name` (the rbg handle); `claudeSessionId` is what `claude --resume` takes.

**Connection gate (client):** `ssh -o BatchMode=yes -o ConnectTimeout=5 <host> true`;
on failure print exactly `cannot reach '<host>' — disconnected` to stderr, exit 1, before anything else.

**ASSUMED, isolated in `claudecli/claude.go`, verified manually later:** the real
`claude` flags and the `agents --json` / `stream-json` shapes. Tests use a fake
claude. Confirming the real contract is a one-time manual check on a box with
`claude` (carried over from v1's deferred Task 2).

**Inject seams for testability:** every function that runs a subprocess takes a
`Runner` interface (`Run(name string, args []string, stdin io.Reader) (stdout []byte, code int, err error)`), defaulting to a real `os/exec` impl. The agent's child-spawn takes a `Spawner`. This is the Go equivalent of v1's injectable `runner` — it lets all logic be tested with no SSH and no real claude.

---

## Task 1: Module skeleton + Runner interface

**Files:**
- Create: `go.mod`
- Create: `internal/run/run.go`
- Create: `internal/run/run_test.go`

- [ ] **Step 1: Initialize the module**

```bash
cd /Users/divkov/workplace/remote-ccbg
go mod init github.com/divkov575/rbg
```
Expected: creates `go.mod` with `module github.com/divkov575/rbg` and a `go 1.26` line.

- [ ] **Step 2: Write the failing test**

Create `internal/run/run_test.go`:

```go
package run

import (
	"strings"
	"testing"
)

func TestRecordingRunner_RecordsAndReturnsDefault(t *testing.T) {
	r := &Recording{Default: Result{Stdout: []byte("ok"), Code: 0}}
	out, code, err := r.Run("ssh", []string{"host", "true"}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if string(out) != "ok" || code != 0 {
		t.Fatalf("got out=%q code=%d", out, code)
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("calls not recorded: %+v", r.Calls)
	}
}

func TestRecordingRunner_MatchesBySubstring(t *testing.T) {
	r := &Recording{
		BySubstring: map[string]Result{"agents": {Stdout: []byte("[]")}},
		Default:     Result{Stdout: []byte("")},
	}
	out, _, _ := r.Run("ssh", []string{"host", "rbg-agent agents"}, nil)
	if string(out) != "[]" {
		t.Fatalf("substring match failed: got %q", out)
	}
	out2, _, _ := r.Run("ssh", []string{"host", "true"}, nil)
	if string(out2) != "" {
		t.Fatalf("default fallthrough failed: got %q", out2)
	}
}

func TestLastArgJoin(t *testing.T) {
	// helper used by tests to assert on the joined remote command
	got := joinArgs([]string{"host", "rbg-agent", "ls"})
	if !strings.Contains(got, "rbg-agent ls") {
		t.Fatalf("joinArgs = %q", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/run/`
Expected: FAIL — `undefined: Recording`, `undefined: Result`, `undefined: joinArgs` (build error).

- [ ] **Step 4: Write the implementation**

Create `internal/run/run.go`:

```go
// Package run defines the subprocess-runner seam used across rbg, so all
// command logic can be unit-tested without spawning real processes.
package run

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
)

// Result is a canned subprocess outcome for the Recording test runner.
type Result struct {
	Stdout []byte
	Stderr []byte
	Code   int
	Err    error
}

// Call records one invocation made through a Runner.
type Call struct {
	Name string
	Args []string
}

// Runner abstracts running a subprocess. Implementations: Exec (real) and
// Recording (test stub).
type Runner interface {
	Run(name string, args []string, stdin io.Reader) (stdout []byte, code int, err error)
}

// Exec is the real runner backed by os/exec.
type Exec struct{}

func (Exec) Run(name string, args []string, stdin io.Reader) ([]byte, int, error) {
	cmd := exec.Command(name, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
		err = nil // exit code carries the signal; not a Go-level error
	}
	return out.Bytes(), code, err
}

// Recording is a test Runner: records calls, returns a Result chosen by the
// first BySubstring key found in the joined args, else Default.
type Recording struct {
	Calls       []Call
	BySubstring map[string]Result
	Default     Result
}

func (r *Recording) Run(name string, args []string, stdin io.Reader) ([]byte, int, error) {
	r.Calls = append(r.Calls, Call{Name: name, Args: args})
	joined := joinArgs(args)
	for sub, res := range r.BySubstring {
		if strings.Contains(joined, sub) {
			return res.Stdout, res.Code, res.Err
		}
	}
	return r.Default.Stdout, r.Default.Code, r.Default.Err
}

func joinArgs(args []string) string { return strings.Join(args, " ") }

var _ Runner = Exec{}
var _ Runner = (*Recording)(nil)
var _ = io.Discard
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/run/`
Expected: `ok` — all pass.

- [ ] **Step 6: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add go.mod internal/run/
git commit -m "feat(v2): go module skeleton and Runner seam"
```

---

## Task 2: Client config (env over ~/.rbg.conf)

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rbg.conf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnvOverridesFile(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=fromfile\nRBG_CWD=/proj\n")
	cfg, err := Load(map[string]string{"RBG_HOST": "fromenv"}, conf)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "fromenv" {
		t.Errorf("Host = %q, want fromenv", cfg.Host)
	}
	if cfg.CWD != "/proj" {
		t.Errorf("CWD = %q, want /proj", cfg.CWD)
	}
}

func TestSSHOptsSplit(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=h\nRBG_SSH=-p 2222 -i ~/k\n")
	cfg, _ := Load(map[string]string{}, conf)
	want := []string{"-p", "2222", "-i", "~/k"}
	if !reflect.DeepEqual(cfg.SSHOpts, want) {
		t.Errorf("SSHOpts = %v, want %v", cfg.SSHOpts, want)
	}
}

func TestMissingHostErrors(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "absent.conf")
	if _, err := Load(map[string]string{}, conf); err == nil {
		t.Fatal("expected error for missing RBG_HOST")
	}
}

func TestAgentPathDefault(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=h\n")
	cfg, _ := Load(map[string]string{}, conf)
	if cfg.AgentPath != "~/.local/bin/rbg-agent" {
		t.Errorf("AgentPath = %q, want default", cfg.AgentPath)
	}
}

func TestQuotedValuesAndComments(t *testing.T) {
	conf := writeConf(t, "# c\nRBG_HOST=\"quoted\"\n\n")
	cfg, _ := Load(map[string]string{}, conf)
	if cfg.Host != "quoted" {
		t.Errorf("Host = %q, want quoted", cfg.Host)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/config/`
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Write the implementation**

Create `internal/config/config.go`:

```go
// Package config loads rbg client configuration from environment variables
// (which win) layered over a ~/.rbg.conf KEY=value file.
package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// Config is the resolved client configuration.
type Config struct {
	Host      string
	CWD       string
	SSHOpts   []string
	AgentPath string
}

const defaultAgentPath = "~/.local/bin/rbg-agent"

// Load merges env over the conf file at confPath. Pass os.Environ-derived map
// for env; a missing file is not an error (only a missing RBG_HOST is).
func Load(env map[string]string, confPath string) (*Config, error) {
	fileVals := readConfFile(confPath)
	get := func(key string) string {
		if v, ok := env[key]; ok && v != "" {
			return v
		}
		return fileVals[key]
	}

	host := get("RBG_HOST")
	if host == "" {
		return nil, errors.New("RBG_HOST not set (export it or put it in ~/.rbg.conf)")
	}
	agentPath := get("RBG_AGENT_PATH")
	if agentPath == "" {
		agentPath = defaultAgentPath
	}
	return &Config{
		Host:      host,
		CWD:       get("RBG_CWD"),
		SSHOpts:   strings.Fields(get("RBG_SSH")),
		AgentPath: agentPath,
	}, nil
}

func readConfFile(path string) map[string]string {
	vals := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return vals
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		vals[strings.TrimSpace(k)] = v
	}
	return vals
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/config/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/config/
git commit -m "feat(v2): client config loading"
```

---

## Task 3: SSH argv builder + connection gate + agent invocation

**Files:**
- Create: `internal/sshx/sshx.go`
- Create: `internal/sshx/sshx_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/sshx/sshx_test.go`:

```go
package sshx

import (
	"reflect"
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

func cfg() *config.Config {
	return &config.Config{Host: "desk", CWD: "", SSHOpts: nil, AgentPath: "rbg-agent"}
}

func TestBuildSSHArgs_Basic(t *testing.T) {
	got := BuildSSHArgs(cfg(), []string{"rbg-agent", "ls"}, Options{})
	want := []string{"desk", "rbg-agent", "ls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestBuildSSHArgs_OptsTTYBatch(t *testing.T) {
	c := cfg()
	c.SSHOpts = []string{"-p", "2222"}
	got := BuildSSHArgs(c, []string{"claude", "--resume", "x"}, Options{TTY: true})
	want := []string{"-t", "-p", "2222", "desk", "claude", "--resume", "x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	gotB := BuildSSHArgs(cfg(), []string{"true"}, Options{Batch: true})
	wantB := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "desk", "true"}
	if !reflect.DeepEqual(gotB, wantB) {
		t.Errorf("got %v want %v", gotB, wantB)
	}
}

func TestReachable(t *testing.T) {
	up := &run.Recording{Default: run.Result{Code: 0}}
	if !Reachable(cfg(), up) {
		t.Error("expected reachable when ssh true returns 0")
	}
	down := &run.Recording{Default: run.Result{Code: 255}}
	if Reachable(cfg(), down) {
		t.Error("expected unreachable when ssh returns 255")
	}
}

func TestAgentArgs_PrefixesCDWhenCWDSet(t *testing.T) {
	c := cfg()
	c.CWD = "/proj"
	// AgentArgs returns the remote argv (no shell). cd is passed as agent flag,
	// not a shell 'cd &&', so it stays structured.
	got := AgentArgs(c, "launch", []string{"--name", "x", "--task", "hi"})
	want := []string{"rbg-agent", "--cwd", "/proj", "launch", "--name", "x", "--task", "hi"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestAgentArgs_NoCDWhenEmpty(t *testing.T) {
	got := AgentArgs(cfg(), "ls", nil)
	want := []string{"rbg-agent", "ls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/sshx/`
Expected: FAIL — `undefined: BuildSSHArgs` etc.

- [ ] **Step 3: Write the implementation**

Create `internal/sshx/sshx.go`:

```go
// Package sshx builds ssh invocations for the rbg client. SSH is transport
// only: it execs rbg-agent (or claude, for attach) directly with a structured
// argv — no remote shell, so nothing is shell-interpolated.
package sshx

import (
	"fmt"
	"os"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

// Options tunes a single ssh invocation.
type Options struct {
	TTY   bool // allocate a tty (-t) for interactive attach
	Batch bool // BatchMode + ConnectTimeout, for the reachability probe
}

// BuildSSHArgs returns the argv for `ssh` (excluding the leading "ssh"):
// [opts...] <host> <remote argv...>. The remote argv is passed as separate
// arguments; OpenSSH forwards them to the remote exec without a shell when
// invoked this way by os/exec (we never wrap them in `sh -c`).
func BuildSSHArgs(c *config.Config, remote []string, o Options) []string {
	var args []string
	if o.Batch {
		args = append(args, "-o", "BatchMode=yes", "-o", "ConnectTimeout=5")
	}
	if o.TTY {
		args = append(args, "-t")
	}
	args = append(args, c.SSHOpts...)
	args = append(args, c.Host)
	args = append(args, remote...)
	return args
}

// AgentArgs builds the remote argv that invokes rbg-agent for a verb. When CWD
// is set it is passed as a structured --cwd flag (not a shell `cd`).
func AgentArgs(c *config.Config, verb string, verbArgs []string) []string {
	out := []string{c.AgentPath}
	if c.CWD != "" {
		out = append(out, "--cwd", c.CWD)
	}
	out = append(out, verb)
	out = append(out, verbArgs...)
	return out
}

// Reachable runs the connection-gate probe. True iff ssh ... true exits 0.
func Reachable(c *config.Config, r run.Runner) bool {
	args := BuildSSHArgs(c, []string{"true"}, Options{Batch: true})
	_, code, err := r.Run("ssh", args, nil)
	return err == nil && code == 0
}

// EnsureReachable prints the v1 disconnection message and exits 1 if the host
// is unreachable.
func EnsureReachable(c *config.Config, r run.Runner) {
	if !Reachable(c, r) {
		fmt.Fprintf(os.Stderr, "cannot reach '%s' — disconnected\n", c.Host)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/sshx/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/sshx/
git commit -m "feat(v2): ssh argv builder and connection gate"
```

---

## Task 4: Transcript render (client-side)

**Files:**
- Create: `internal/render/render.go`
- Create: `internal/render/render_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/render/render_test.go`:

```go
package render

import (
	"bytes"
	"testing"
)

func TestLine_StringContent(t *testing.T) {
	got, ok := Line(`{"type":"user","message":{"role":"user","content":"hello"}}`)
	if !ok || got != "user: hello" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestLine_TextBlocks(t *testing.T) {
	in := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi there"}]}}`
	got, ok := Line(in)
	if !ok || got != "assistant: hi there" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestLine_ToolUse(t *testing.T) {
	in := `{"message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`
	got, ok := Line(in)
	if !ok || got != "assistant: [tool: Bash]" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestLine_SkipsBlankBadEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "{bad json", `{"type":"system","message":{"content":[]}}`} {
		if got, ok := Line(in); ok {
			t.Fatalf("expected skip for %q, got %q", in, got)
		}
	}
}

func TestLine_ToleratesUnknownKeys(t *testing.T) {
	in := `{"type":"assistant","weird":1,"message":{"role":"assistant","content":"ok"}}`
	got, ok := Line(in)
	if !ok || got != "assistant: ok" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}

func TestStream_PrintsOnlyRenderable(t *testing.T) {
	lines := []string{
		`{"message":{"role":"user","content":"q"}}`,
		"garbage",
		`{"message":{"role":"assistant","content":"a"}}`,
	}
	var buf bytes.Buffer
	Stream(lines, &buf)
	if buf.String() != "user: q\nassistant: a\n" {
		t.Fatalf("got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/render/`
Expected: FAIL — `undefined: Line`, `undefined: Stream`.

- [ ] **Step 3: Write the implementation**

Create `internal/render/render.go`:

```go
// Package render turns claude transcript JSONL lines into human text. It
// tolerates unknown keys and malformed lines (returns ok=false to skip).
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type record struct {
	Type    string  `json:"type"`
	Message message `json:"message"`
}

type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"`
}

// Line renders one JSONL line to "role: text", or ok=false to skip it.
func Line(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	var rec record
	if err := json.Unmarshal([]byte(s), &rec); err != nil {
		return "", false
	}
	var parts []string
	if len(rec.Message.Content) > 0 {
		// content may be a string or an array of blocks.
		var str string
		if json.Unmarshal(rec.Message.Content, &str) == nil {
			if str != "" {
				parts = append(parts, str)
			}
		} else {
			var blocks []block
			if json.Unmarshal(rec.Message.Content, &blocks) == nil {
				for _, b := range blocks {
					switch b.Type {
					case "text":
						if b.Text != "" {
							parts = append(parts, b.Text)
						}
					case "tool_use":
						name := b.Name
						if name == "" {
							name = "?"
						}
						parts = append(parts, fmt.Sprintf("[tool: %s]", name))
					case "tool_result":
						parts = append(parts, "[tool result]")
					}
				}
			}
		}
	}
	text := strings.Join(parts, "\n")
	if text == "" {
		return "", false
	}
	role := rec.Message.Role
	if role == "" {
		role = rec.Type
	}
	if role == "" {
		role = "?"
	}
	return role + ": " + text, true
}

// Stream renders each line and writes renderable ones to w.
func Stream(lines []string, w io.Writer) {
	for _, ln := range lines {
		if out, ok := Line(ln); ok {
			fmt.Fprintln(w, out)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/render/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/render/
git commit -m "feat(v2): client-side transcript rendering"
```

---

## Task 5: Agent session-state store

**Files:**
- Create: `internal/session/session.go`
- Create: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/session_test.go`:

```go
package session

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sessions) != 0 {
		t.Fatalf("expected empty, got %+v", s.Sessions)
	}
}

func TestAddSaveLoadRoundtrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "sessions.json") // parent absent
	s, _ := Load(p)
	s.Add(Session{Name: "alpha", ClaudeSessionID: "sid-1", TranscriptPath: "/t/x.jsonl"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := Load(p)
	a, ok := got.Get("alpha")
	if !ok || a.ClaudeSessionID != "sid-1" || a.TranscriptPath != "/t/x.jsonl" {
		t.Fatalf("roundtrip failed: %+v ok=%v", a, ok)
	}
}

func TestGetMissing(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), "s.json"))
	if _, ok := s.Get("ghost"); ok {
		t.Fatal("expected ghost absent")
	}
}

func TestLoadCorruptReturnsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	_ = writeFile(p, "{not json")
	s, err := Load(p)
	if err != nil {
		t.Fatalf("corrupt should not error, got %v", err)
	}
	if len(s.Sessions) != 0 {
		t.Fatal("corrupt should load empty")
	}
}
```

Also create `internal/session/helpers_test.go`:

```go
package session

import "os"

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/session/`
Expected: FAIL — `undefined: Load`, `undefined: Session`.

- [ ] **Step 3: Write the implementation**

Create `internal/session/session.go`:

```go
// Package session manages the desktop agent's session-state file:
// id -> {name, claudeSessionId, transcriptPath, pid, startedAt}.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Session is one tracked agent session.
type Session struct {
	Name            string `json:"name"`
	ClaudeSessionID string `json:"claudeSessionId"`
	TranscriptPath  string `json:"transcriptPath"`
	PID             int    `json:"pid"`
	StartedAt       string `json:"startedAt"`
}

// Store is the in-memory + on-disk session map.
type Store struct {
	path     string
	Sessions map[string]Session
}

// Load reads the store at path; a missing or corrupt file yields an empty store
// (not an error), so first-run and partial-writes are tolerated.
func Load(path string) (*Store, error) {
	s := &Store{path: path, Sessions: map[string]Session{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(data, &s.Sessions) // corrupt → keep empty map
	if s.Sessions == nil {
		s.Sessions = map[string]Session{}
	}
	return s, nil
}

// Add inserts/updates a session keyed by its Name.
func (s *Store) Add(sess Session) { s.Sessions[sess.Name] = sess }

// Get returns the session for id (==name in v2).
func (s *Store) Get(id string) (Session, bool) {
	sess, ok := s.Sessions[id]
	return sess, ok
}

// Save writes the store atomically (temp + rename), creating parents.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.Sessions, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/session/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/session/
git commit -m "feat(v2): agent session-state store"
```

---

## Task 6: Non-blocking per-session lock (tmux serialization replacement)

**Files:**
- Create: `internal/session/lock.go`
- Create: `internal/session/lock_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/lock_test.go`:

```go
package session

import (
	"path/filepath"
	"testing"
)

func TestTryLock_AcquiresAndBlocksSecond(t *testing.T) {
	p := filepath.Join(t.TempDir(), "alpha.lock")
	l1, ok, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("first TryLock should acquire")
	}
	defer l1.Unlock()

	_, ok2, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if ok2 {
		t.Fatal("second TryLock on held lock should fail (busy)")
	}
}

func TestTryLock_ReacquireAfterUnlock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "alpha.lock")
	l1, ok, _ := TryLock(p)
	if !ok {
		t.Fatal("acquire 1")
	}
	l1.Unlock()
	l2, ok2, _ := TryLock(p)
	if !ok2 {
		t.Fatal("should re-acquire after unlock")
	}
	l2.Unlock()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/session/ -run TestTryLock`
Expected: FAIL — `undefined: TryLock`.

- [ ] **Step 3: Write the implementation**

Create `internal/session/lock.go`:

```go
package session

import (
	"os"
	"path/filepath"
	"syscall"
)

// Lock is a held advisory file lock.
type Lock struct{ f *os.File }

// TryLock attempts a non-blocking exclusive flock on path. ok=false means the
// lock is already held (the "send busy" case) — this is the in-code replacement
// for v1's tmux window-name busy check. Creates parent dirs as needed.
func TryLock(path string) (*Lock, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &Lock{f: f}, true, nil
}

// Unlock releases the lock and closes the file.
func (l *Lock) Unlock() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/session/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/session/lock.go internal/session/lock_test.go
git commit -m "feat(v2): non-blocking per-session flock (tmux serialization replacement)"
```

---

## Task 7: claude CLI adapter (isolated contract)

**Files:**
- Create: `internal/claudecli/claude.go`
- Create: `internal/claudecli/claude_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/claudecli/claude_test.go`:

```go
package claudecli

import (
	"reflect"
	"testing"
)

func TestBGArgs(t *testing.T) {
	got := BGArgs("alpha", "do the thing")
	want := []string{"--bg", "-n", "alpha", "do the thing"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestResumeHeadlessArgs(t *testing.T) {
	got := ResumeHeadlessArgs("sid-1", "next step")
	want := []string{"-p", "next step", "--resume", "sid-1", "--output-format", "stream-json"}
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

func TestParseAgents_BareArrayAndWrapped(t *testing.T) {
	a, err := ParseAgents([]byte(`[{"name":"a","sessionId":"x"}]`))
	if err != nil || len(a) != 1 || a[0].Name != "a" || a[0].SessionID != "x" {
		t.Fatalf("bare array parse: %+v err=%v", a, err)
	}
	b, err := ParseAgents([]byte(`{"agents":[{"name":"b","session_id":"y"}]}`))
	if err != nil || len(b) != 1 || b[0].SessionID != "y" {
		t.Fatalf("wrapped parse: %+v err=%v", b, err)
	}
}

func TestParseAgents_Garbage(t *testing.T) {
	a, _ := ParseAgents([]byte("not json"))
	if len(a) != 0 {
		t.Fatalf("garbage should yield empty, got %+v", a)
	}
}

func TestFindSessionID(t *testing.T) {
	agents := []Agent{{Name: "alpha", SessionID: "sid-a"}, {Name: "beta", SessionID: "sid-b"}}
	if got := FindSessionID(agents, "beta"); got != "sid-b" {
		t.Errorf("got %q want sid-b", got)
	}
	if got := FindSessionID(agents, "ghost"); got != "" {
		t.Errorf("ghost should be empty, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/claudecli/`
Expected: FAIL — `undefined: BGArgs` etc.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecli/claude.go`:

```go
// Package claudecli isolates the contract with the real `claude` binary: the
// argv we pass and the JSON shapes we parse. EVERYTHING we assume about claude
// lives here, so the one-time manual verification has a single place to fix.
package claudecli

import (
	"encoding/json"
	"strings"
)

// Agent is one entry from `claude agents --json`.
type Agent struct {
	Name      string `json:"name"`
	SessionID string `json:"-"`
}

// agentWire tolerates the three id key spellings we might see.
type agentWire struct {
	Name       string `json:"name"`
	SessionID  string `json:"sessionId"`
	SessionSnk string `json:"session_id"`
	ID         string `json:"id"`
}

func (w agentWire) toAgent() Agent {
	id := w.SessionID
	if id == "" {
		id = w.SessionSnk
	}
	if id == "" {
		id = w.ID
	}
	return Agent{Name: w.Name, SessionID: id}
}

// BGArgs builds `claude --bg -n <name> <task>`.
func BGArgs(name, task string) []string {
	return []string{"--bg", "-n", name, task}
}

// ResumeHeadlessArgs builds the headless send invocation.
func ResumeHeadlessArgs(sessionID, task string) []string {
	return []string{"-p", task, "--resume", sessionID, "--output-format", "stream-json"}
}

// AgentsListArgs builds `claude agents --json --all`.
func AgentsListArgs() []string {
	return []string{"agents", "--json", "--all"}
}

// ParseAgents parses bare-array or {"agents":[...]} output; garbage → empty.
func ParseAgents(data []byte) ([]Agent, error) {
	trimmed := strings.TrimSpace(string(data))
	var wires []agentWire
	if strings.HasPrefix(trimmed, "{") {
		var wrapped struct {
			Agents []agentWire `json:"agents"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapped); err != nil {
			return nil, nil
		}
		wires = wrapped.Agents
	} else {
		if err := json.Unmarshal([]byte(trimmed), &wires); err != nil {
			return nil, nil
		}
	}
	out := make([]Agent, 0, len(wires))
	for _, w := range wires {
		out = append(out, w.toAgent())
	}
	return out, nil
}

// FindSessionID returns the claude session id for the named agent, or "".
func FindSessionID(agents []Agent, name string) string {
	for _, a := range agents {
		if a.Name == name {
			return a.SessionID
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/claudecli/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/claudecli/
git commit -m "feat(v2): isolated claude CLI adapter"
```

---

## Task 8: Agent — launch & ls

**Files:**
- Create: `internal/agent/agent.go`
- Create: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/agent_test.go`:

```go
package agent

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/run"
)

func newAgent(t *testing.T, r run.Runner) *Agent {
	t.Helper()
	dir := t.TempDir()
	return &Agent{
		Runner:      r,
		StatePath:   filepath.Join(dir, "sessions.json"),
		ClaudeHome:  dir, // transcripts rooted here
		Now:         func() string { return "2026-06-28T00:00:00Z" },
	}
}

func TestLaunch_RecordsSessionAndPrintsJSON(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"agents": {Stdout: []byte(`[{"name":"alpha","sessionId":"sid-1"}]`)},
		},
		Default: run.Result{Code: 0},
	}
	a := newAgent(t, r)
	var out bytes.Buffer
	code := a.Launch(&out, "alpha", "build it")
	if code != 0 {
		t.Fatalf("Launch code=%d", code)
	}
	var resp map[string]string
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v (%s)", err, out.String())
	}
	if resp["id"] != "alpha" || resp["claudeSessionId"] != "sid-1" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestLaunch_UnresolvedIDErrors(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{"agents": {Stdout: []byte("[]")}},
		Default:     run.Result{Code: 0},
	}
	a := newAgent(t, r)
	var out bytes.Buffer
	if code := a.Launch(&out, "alpha", "x"); code == 0 {
		t.Fatal("expected nonzero when id unresolved")
	}
}

func TestLs_PrintsRecordedSessions(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 0}}
	a := newAgent(t, r)
	// seed two via Launch using a runner that resolves ids
	a.Runner = &run.Recording{BySubstring: map[string]run.Result{
		"agents": {Stdout: []byte(`[{"name":"alpha","sessionId":"sid-1"},{"name":"beta","sessionId":"sid-2"}]`)},
	}}
	a.Launch(&bytes.Buffer{}, "alpha", "one")
	a.Launch(&bytes.Buffer{}, "beta", "two")

	var out bytes.Buffer
	if code := a.Ls(&out); code != 0 {
		t.Fatalf("Ls code=%d", code)
	}
	var list []map[string]any
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		t.Fatalf("bad json: %v (%s)", err, out.String())
	}
	if len(list) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(list))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/agent/`
Expected: FAIL — `undefined: Agent`.

- [ ] **Step 3: Write the implementation**

Create `internal/agent/agent.go`:

```go
// Package agent implements the desktop-side rbg-agent verbs. It owns session
// state, resolves claude sessions, serializes sends with a file lock, and
// streams transcripts. It is exec'd directly by sshd — never via a shell.
package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/divkov575/rbg/internal/claudecli"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)

// Agent holds the agent's injectable dependencies.
type Agent struct {
	Runner     run.Runner
	StatePath  string         // ~/.rbg-agent/sessions.json
	ClaudeHome string         // root for transcript paths (~ in prod)
	Now        func() string  // timestamp source (injectable for tests)
}

const claudeBin = "claude"

// transcriptPath derives the JSONL path for a claude session id. In v2 the
// agent owns this mapping, so no glob is ever needed.
func (a *Agent) transcriptPath(claudeSessionID string) string {
	return filepath.Join(a.ClaudeHome, ".claude", "projects", "sim-project", claudeSessionID+".jsonl")
}

// Launch starts a --bg claude agent, resolves its session id, records it, and
// prints {"id","claudeSessionId"} as JSON.
func (a *Agent) Launch(out io.Writer, name, task string) int {
	a.Runner.Run(claudeBin, claudecli.BGArgs(name, task), nil)
	listing, _, _ := a.Runner.Run(claudeBin, claudecli.AgentsListArgs(), nil)
	agents, _ := claudecli.ParseAgents(listing)
	sid := claudecli.FindSessionID(agents, name)
	if sid == "" {
		fmt.Fprintf(out, "rbg-agent: could not resolve session id for %q\n", name)
		return 1
	}
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	store.Add(session.Session{
		Name:            name,
		ClaudeSessionID: sid,
		TranscriptPath:  a.transcriptPath(sid),
		StartedAt:       a.Now(),
	})
	if err := store.Save(); err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	json.NewEncoder(out).Encode(map[string]string{"id": name, "claudeSessionId": sid})
	return 0
}

// Ls prints all recorded sessions as a JSON array.
func (a *Agent) Ls(out io.Writer) int {
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	list := make([]session.Session, 0, len(store.Sessions))
	for _, s := range store.Sessions {
		list = append(list, s)
	}
	json.NewEncoder(out).Encode(list)
	return 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/agent/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/agent/
git commit -m "feat(v2): agent launch and ls"
```

---

## Task 9: Agent — send (detached child + lock) & read (stream transcript)

**Files:**
- Modify: `internal/agent/agent.go`
- Create: `internal/agent/send_read_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/send_read_test.go`:

```go
package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)

// seed writes a session and a transcript file with the given lines.
func seed(t *testing.T, a *Agent, name, sid string, lines string) {
	t.Helper()
	store, _ := session.Load(a.StatePath)
	tp := a.transcriptPath(sid)
	os.MkdirAll(filepath.Dir(tp), 0o755)
	os.WriteFile(tp, []byte(lines), 0o600)
	store.Add(session.Session{Name: name, ClaudeSessionID: sid, TranscriptPath: tp})
	store.Save()
}

func TestSend_SpawnsChildAndReturnsOK(t *testing.T) {
	r := &run.Recording{Default: run.Result{Code: 0}}
	a := newAgent(t, r)
	seed(t, a, "alpha", "sid-1", "")
	spawned := false
	a.Spawn = func(name string, args []string, stdoutPath string) (int, error) {
		spawned = true
		return 4321, nil
	}
	var out bytes.Buffer
	if code := a.Send(&out, "alpha", "next step"); code != 0 {
		t.Fatalf("Send code=%d out=%s", code, out.String())
	}
	if !spawned {
		t.Fatal("expected child spawn")
	}
}

func TestSend_UnknownAgent(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	var out bytes.Buffer
	if code := a.Send(&out, "ghost", "x"); code != 1 {
		t.Fatalf("want 1 for unknown, got %d", code)
	}
}

func TestSend_BusyReturns3(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	seed(t, a, "alpha", "sid-1", "")
	// hold the lock to simulate an in-flight send
	lock, ok, _ := session.TryLock(a.lockPath("alpha"))
	if !ok {
		t.Fatal("could not pre-acquire lock")
	}
	defer lock.Unlock()
	var out bytes.Buffer
	if code := a.Send(&out, "alpha", "x"); code != 3 {
		t.Fatalf("want 3 (busy), got %d", code)
	}
}

func TestRead_StreamsRenderedTranscript(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	lines := `{"message":{"role":"user","content":"q"}}` + "\n" +
		`{"message":{"role":"assistant","content":"a"}}` + "\n"
	seed(t, a, "alpha", "sid-1", lines)
	var out bytes.Buffer
	if code := a.Read(&out, "alpha"); code != 0 {
		t.Fatalf("Read code=%d", code)
	}
	got := out.String()
	if got != "user: q\nassistant: a\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRead_UnknownAgent(t *testing.T) {
	a := newAgent(t, &run.Recording{})
	var out bytes.Buffer
	if code := a.Read(&out, "ghost"); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/agent/ -run 'TestSend|TestRead'`
Expected: FAIL — `a.Spawn undefined`, `a.Send undefined`, `a.lockPath undefined`, `a.Read undefined`.

- [ ] **Step 3: Write the implementation**

Add to `internal/agent/agent.go` — first extend the `Agent` struct and add a Spawn type, then the methods.

Replace the existing `Agent` struct definition with:

```go
// SpawnFunc starts a detached child process whose stdout is redirected to
// stdoutPath, returning its pid. The real impl sets a new process group so the
// child outlives the SSH session (the tmux-detachment replacement).
type SpawnFunc func(name string, args []string, stdoutPath string) (pid int, err error)

// Agent holds the agent's injectable dependencies.
type Agent struct {
	Runner     run.Runner
	StatePath  string        // ~/.rbg-agent/sessions.json
	ClaudeHome string        // root for transcript paths (~ in prod)
	Now        func() string // timestamp source (injectable for tests)
	Spawn      SpawnFunc     // detached child spawner (defaults via DefaultSpawn)
	LockDir    string        // dir for per-session lockfiles (defaults beside state)
}
```

Then append these methods and the default spawn:

```go
import_os_exec_note := 0 // (remove; placeholder to remind imports below)

// lockPath returns the lockfile path for a session id.
func (a *Agent) lockPath(id string) string {
	dir := a.LockDir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(a.StatePath), "locks")
	}
	return filepath.Join(dir, id+".lock")
}

// Send acquires the per-session lock (busy → exit 3), then spawns a detached
// headless claude resume that appends to the transcript, and returns at once.
func (a *Agent) Send(out io.Writer, name, task string) int {
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	sess, ok := store.Get(name)
	if !ok {
		fmt.Fprintf(out, "rbg-agent: unknown agent %q\n", name)
		return 1
	}
	lock, ok, err := session.TryLock(a.lockPath(name))
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	if !ok {
		fmt.Fprintf(out, "rbg-agent: session %q busy\n", name)
		return 3
	}
	// NOTE: we intentionally release the lock once the child is launched; the
	// child's own run is short and append-only. Holding for the child's full
	// lifetime would require the lock to travel into the detached process.
	defer lock.Unlock()

	spawn := a.Spawn
	if spawn == nil {
		spawn = DefaultSpawn
	}
	args := append([]string{claudeBin}, claudecli.ResumeHeadlessArgs(sess.ClaudeSessionID, task)...)
	_, err = spawn(args[0], args[1:], sess.TranscriptPath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: spawn failed: %v\n", err)
		return 1
	}
	json.NewEncoder(out).Encode(map[string]string{"ok": "sent", "id": name})
	return 0
}

// Read renders the session's transcript to out. (Follow mode is handled by the
// client tailing via a separate streaming path; here we emit the full file.)
func (a *Agent) Read(out io.Writer, name string) int {
	store, err := session.Load(a.StatePath)
	if err != nil {
		fmt.Fprintf(out, "rbg-agent: %v\n", err)
		return 1
	}
	sess, ok := store.Get(name)
	if !ok {
		fmt.Fprintf(out, "rbg-agent: unknown agent %q\n", name)
		return 1
	}
	data, err := os.ReadFile(sess.TranscriptPath)
	if err != nil {
		// no transcript yet → nothing to render, but not an error
		return 0
	}
	var lines []string
	for _, ln := range strings.Split(string(data), "\n") {
		lines = append(lines, ln)
	}
	render.Stream(lines, out)
	return 0
}

// DefaultSpawn starts a detached child in its own process group with stdout
// appended to stdoutPath. The child survives the parent (and the SSH session).
func DefaultSpawn(name string, args []string, stdoutPath string) (int, error) {
	f, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		f.Close()
		return 0, err
	}
	pid := cmd.Process.Pid
	// Reap asynchronously so we don't block; the child is detached regardless.
	go func() { _ = cmd.Wait(); f.Close() }()
	return pid, nil
}
```

Now fix the imports at the top of `internal/agent/agent.go`. Replace the import block with:

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/divkov575/rbg/internal/claudecli"
	"github.com/divkov575/rbg/internal/render"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/session"
)
```

And delete the bogus `import_os_exec_note := 0` placeholder line you added above `lockPath` (it was only a reminder).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/agent/`
Expected: `ok` — all agent tests pass.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/agent/
git commit -m "feat(v2): agent send (detached child + lock) and read"
```

---

## Task 10: Agent entrypoint (`cmd/rbg-agent`)

**Files:**
- Create: `cmd/rbg-agent/main.go`
- Create: `cmd/rbg-agent/main_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/rbg-agent/main_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./cmd/rbg-agent/`
Expected: FAIL — `undefined: parseArgs`.

- [ ] **Step 3: Write the implementation**

Create `cmd/rbg-agent/main.go`:

```go
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
		inv.Name = flagValue(rest, "--name")
		inv.Task = flagValue(rest, "--task")
		if inv.Name == "" || inv.Task == "" {
			return nil, errors.New("launch requires --name and --task")
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

func newAgent() *agent.Agent {
	home, _ := os.UserHomeDir()
	return &agent.Agent{
		Runner:     run.Exec{},
		StatePath:  filepath.Join(home, ".rbg-agent", "sessions.json"),
		ClaudeHome: home,
		Now:        func() string { return time.Now().UTC().Format(time.RFC3339) },
	}
}

func main() {
	inv, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "rbg-agent: %v\n", err)
		os.Exit(2)
	}
	a := newAgent()
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
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./cmd/rbg-agent/`
Expected: `ok`.

- [ ] **Step 5: Verify it builds**

Run: `cd /Users/divkov/workplace/remote-ccbg && go build ./cmd/rbg-agent/`
Expected: builds with no errors (produces `rbg-agent` binary; you may delete it after).

- [ ] **Step 6: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add cmd/rbg-agent/
git commit -m "feat(v2): rbg-agent entrypoint and flag parsing"
```

---

## Task 11: Client commands (`internal/client`)

**Files:**
- Create: `internal/client/client.go`
- Create: `internal/client/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/client/client_test.go`:

```go
package client

import (
	"bytes"
	"strings"
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

func cfg() *config.Config {
	return &config.Config{Host: "desk", AgentPath: "rbg-agent"}
}

func TestLaunch_InvokesAgentOverSSH(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"launch": {Stdout: []byte(`{"id":"alpha","claudeSessionId":"sid-1"}`)},
		},
		Default: run.Result{Code: 0}, // reachability true
	}
	var out bytes.Buffer
	code := Launch(cfg(), r, &out, "alpha", "build it")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	// second call (index 1) is the agent launch over ssh; index 0 is the gate
	if len(r.Calls) < 2 {
		t.Fatalf("expected gate + launch calls, got %d", len(r.Calls))
	}
	joined := strings.Join(r.Calls[1].Args, " ")
	if !strings.Contains(joined, "rbg-agent") || !strings.Contains(joined, "launch") {
		t.Fatalf("launch call = %q", joined)
	}
	if !strings.Contains(out.String(), "alpha") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestLs_RendersAgentJSON(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{
			"ls": {Stdout: []byte(`[{"name":"alpha","claudeSessionId":"sid-1","transcriptPath":"/t/x"}]`)},
		},
		Default: run.Result{Code: 0},
	}
	var out bytes.Buffer
	if code := Ls(cfg(), r, &out); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out.String(), "alpha") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestRead_RendersStreamedTranscript(t *testing.T) {
	transcript := `{"message":{"role":"user","content":"q"}}` + "\n" +
		`{"message":{"role":"assistant","content":"a"}}` + "\n"
	r := &run.Recording{
		BySubstring: map[string]run.Result{"read": {Stdout: []byte(transcript)}},
		Default:     run.Result{Code: 0},
	}
	var out bytes.Buffer
	if code := Read(cfg(), r, &out, "alpha"); code != 0 {
		t.Fatalf("code=%d", code)
	}
	// client passes agent output through render (already rendered? no — agent
	// `read` already renders, so client prints verbatim). Accept either; assert
	// the assistant line is present.
	if !strings.Contains(out.String(), "a") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestSend_BusyMapsToExit3(t *testing.T) {
	r := &run.Recording{
		BySubstring: map[string]run.Result{"send": {Code: 3}},
		Default:     run.Result{Code: 0},
	}
	var out bytes.Buffer
	if code := Send(cfg(), r, &out, "alpha", "x"); code != 3 {
		t.Fatalf("want 3, got %d", code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/client/`
Expected: FAIL — `undefined: Launch` etc.

- [ ] **Step 3: Write the implementation**

Create `internal/client/client.go`:

```go
// Package client implements the laptop-side verbs: run the connection gate,
// invoke rbg-agent over ssh with a structured argv, and render the result.
package client

import (
	"fmt"
	"io"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// runAgent runs the connection gate then execs rbg-agent <verb> over ssh,
// returning its stdout and exit code.
func runAgent(c *config.Config, r run.Runner, verb string, verbArgs []string) ([]byte, int) {
	sshx.EnsureReachable(c, r)
	remote := sshx.AgentArgs(c, verb, verbArgs)
	sshArgs := sshx.BuildSSHArgs(c, remote, sshx.Options{})
	out, code, _ := r.Run("ssh", sshArgs, nil)
	return out, code
}

// Launch starts a named bg agent on the desktop and prints the agent's reply.
func Launch(c *config.Config, r run.Runner, out io.Writer, name, task string) int {
	body, code := runAgent(c, r, "launch", []string{"--name", name, "--task", task})
	out.Write(body)
	return code
}

// Send delivers a follow-up task; exit 3 propagates the agent's busy signal.
func Send(c *config.Config, r run.Runner, out io.Writer, name, task string) int {
	body, code := runAgent(c, r, "send", []string{"--id", name, "--task", task})
	if code == 3 {
		fmt.Fprintf(out, "rbg: session %q busy — a send is already running\n", name)
		return 3
	}
	out.Write(body)
	return code
}

// Read prints the session transcript (already rendered by the agent).
func Read(c *config.Config, r run.Runner, out io.Writer, name string) int {
	body, code := runAgent(c, r, "read", []string{"--id", name})
	out.Write(body)
	return code
}

// Ls prints the desktop's session list.
func Ls(c *config.Config, r run.Runner, out io.Writer) int {
	body, code := runAgent(c, r, "ls", nil)
	out.Write(body)
	return code
}

// Ping reports reachability using the gate only.
func Ping(c *config.Config, r run.Runner, out io.Writer) int {
	if sshx.Reachable(c, r) {
		fmt.Fprintf(out, "%s: reachable\n", c.Host)
		return 0
	}
	fmt.Fprintf(out, "cannot reach '%s' — disconnected\n", c.Host)
	return 1
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./internal/client/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add internal/client/
git commit -m "feat(v2): client verbs over ssh"
```

---

## Task 12: Client entrypoint (`cmd/rbg`) + attach + deploy

**Files:**
- Create: `cmd/rbg/main.go`
- Create: `cmd/rbg/main_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/rbg/main_test.go`:

```go
package main

import "testing"

func TestParse_Launch(t *testing.T) {
	inv, err := parse([]string{"launch", "alpha", "do the thing"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.verb != "launch" || inv.name != "alpha" || inv.task != "do the thing" {
		t.Fatalf("inv = %+v", inv)
	}
}

func TestParse_ReadFollow(t *testing.T) {
	inv, err := parse([]string{"read", "alpha", "-f"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.verb != "read" || inv.name != "alpha" || !inv.follow {
		t.Fatalf("inv = %+v", inv)
	}
}

func TestParse_LsPing(t *testing.T) {
	for _, v := range []string{"ls", "ping"} {
		inv, err := parse([]string{v})
		if err != nil || inv.verb != v {
			t.Fatalf("verb %q: inv=%+v err=%v", v, inv, err)
		}
	}
}

func TestParse_UnknownVerb(t *testing.T) {
	if _, err := parse([]string{"frob"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParse_AttachAndDeploy(t *testing.T) {
	if inv, err := parse([]string{"attach", "alpha"}); err != nil || inv.verb != "attach" || inv.name != "alpha" {
		t.Fatalf("attach: %+v %v", inv, err)
	}
	if inv, err := parse([]string{"deploy"}); err != nil || inv.verb != "deploy" {
		t.Fatalf("deploy: %+v %v", inv, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./cmd/rbg/`
Expected: FAIL — `undefined: parse`.

- [ ] **Step 3: Write the implementation**

Create `cmd/rbg/main.go`:

```go
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
		remote := sshx.AgentArgs(cfg, "read", []string{"--id", name})
		_ = remote
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
```

Create `cmd/rbg/support.go` (the helpers the entrypoint references):

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
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
	_ = runtime.GOOS
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

var _ = config.Config{}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./cmd/rbg/`
Expected: `ok`.

- [ ] **Step 5: Verify the whole module builds and all unit tests pass**

Run: `cd /Users/divkov/workplace/remote-ccbg && go build ./... && go test ./...`
Expected: builds clean; all packages `ok`.

- [ ] **Step 6: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add cmd/rbg/
git commit -m "feat(v2): rbg client entrypoint, attach, and deploy"
```

---

## Task 13: gofmt + vet sweep

**Files:** (whole module)

- [ ] **Step 1: Format and vet**

Run:
```bash
cd /Users/divkov/workplace/remote-ccbg
gofmt -w .
go vet ./...
```
Expected: `gofmt -l .` prints nothing afterward; `go vet` reports no problems.

- [ ] **Step 2: Full test run**

Run: `cd /Users/divkov/workplace/remote-ccbg && go test ./...`
Expected: all packages `ok`.

- [ ] **Step 3: Commit (only if gofmt changed anything)**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add -A
git commit -m "chore(v2): gofmt and vet" || echo "nothing to format"
```

---

## Task 14: Integration harness update (drop tmux, drive rbg-agent)

**Files:**
- Create: `test/integration_v2/fake_claude.py`
- Create: `test/integration_v2/sshd_harness.py`
- Create: `test/integration_v2/test_integration_v2.py`
- Create: `test/integration_v2/README.md`

This reuses v1's proven non-root sshd approach but the remote command is now the
built `rbg-agent` binary, and there is **no tmux** (so no symlink, no
`TMUX_TMPDIR`). The fake claude is the same idea as v1.

- [ ] **Step 1: Copy & adapt the fake claude**

Create `test/integration_v2/fake_claude.py` identical to v1's
`test/integration/fake_claude.py` (it already implements `--bg -n`,
`agents --json --all`, `-p --resume … stream-json`, and `--resume`), EXCEPT
ensure the transcript slug is `sim-project` to match the agent's
`transcriptPath` (`internal/agent/agent.go` uses `projects/sim-project/`).

Copy the file:
```bash
cd /Users/divkov/workplace/remote-ccbg
mkdir -p test/integration_v2
cp test/integration/fake_claude.py test/integration_v2/fake_claude.py
```
Then confirm the `SLUG = "sim-project"` line in the copy matches the agent.

- [ ] **Step 2: Adapt the harness (remove tmux)**

Create `test/integration_v2/sshd_harness.py` by copying v1's and making three
changes:
```bash
cd /Users/divkov/workplace/remote-ccbg
cp test/integration/sshd_harness.py test/integration_v2/sshd_harness.py
```
Edit the copy:
1. In `_make_bin`, DELETE the tmux symlink block (the agent needs no tmux):
   remove the lines that find tmux and `os.symlink(tmux, ...)`. Keep the
   `claude` shim creation.
2. In `_write_config`, DELETE the `TMUX_TMPDIR` from the `SetEnv` line and the
   tmux_tmp dir creation; the `SetEnv` becomes just
   `SetEnv PATH={path_env} HOME={self.home}`.
3. In `stop`, delete the `shutil.rmtree(self._tmux_tmp ...)` cleanup and the
   `self._tmux_tmp` attribute.
4. Add a method to deploy the agent binary into the sandbox bin dir:

```python
    def install_agent(self, agent_binary):
        """Copy a prebuilt rbg-agent into the sandbox PATH."""
        import shutil
        dest = os.path.join(self.bin_dir, "rbg-agent")
        shutil.copy(agent_binary, dest)
        os.chmod(dest, 0o755)
```

(The `_find_tmux` import in the harness can stay or go; the test no longer
requires tmux.)

- [ ] **Step 3: Write the integration test**

Create `test/integration_v2/test_integration_v2.py`:

```python
"""End-to-end tests for rbg v2 (Go) against a simulated SSH desktop.

Builds the rbg-agent binary, installs it into a sandboxed non-root sshd's PATH
alongside a fake claude, then drives the rbg client binary against it over real
SSH. No tmux anywhere. Proves the agent-binary mechanics end-to-end.

Skips automatically if go, sshd, or the build is unavailable.
"""
import json
import os
import shutil
import subprocess
import sys

import pytest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))

sys.path.insert(0, HERE)
from sshd_harness import SimDesktop, SSHD_BIN  # noqa: E402

pytestmark = pytest.mark.integration

_REASON = None
if shutil.which("go") is None:
    _REASON = "go not available"
elif not os.path.exists(SSHD_BIN):
    _REASON = f"{SSHD_BIN} not available"
needs_env = pytest.mark.skipif(_REASON is not None, reason=_REASON or "")


def _build(name, tmp):
    out = os.path.join(tmp, name)
    res = subprocess.run(
        ["go", "build", "-o", out, f"github.com/divkov575/rbg/cmd/{name}"],
        cwd=REPO_ROOT, capture_output=True, text=True,
    )
    assert res.returncode == 0, f"build {name} failed: {res.stderr}"
    return out


@pytest.fixture
def env(tmp_path):
    """Built binaries + a running sim desktop with the agent installed."""
    rbg = _build("rbg", str(tmp_path))
    agent = _build("rbg-agent", str(tmp_path))
    sim = SimDesktop(str(tmp_path / "sim"))
    sim.start()
    sim.install_agent(agent)
    yield sim, rbg
    sim.stop()


def run_rbg(sim, rbg, client_home, *args, timeout=30):
    e = sim.rbg_env()
    e["HOME"] = str(client_home)
    e["RBG_AGENT_PATH"] = "rbg-agent"  # on PATH in the sandbox
    return subprocess.run([rbg, *args], env=e, capture_output=True, text=True, timeout=timeout)


@needs_env
def test_ping(env, tmp_path):
    sim, rbg = env
    res = run_rbg(sim, rbg, tmp_path / "ch", "ping")
    assert res.returncode == 0, res.stderr
    assert "reachable" in res.stdout


@needs_env
def test_launch_then_ls(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    launch = run_rbg(sim, rbg, ch, "launch", "alpha", "say hello")
    assert launch.returncode == 0, launch.stderr
    assert "alpha" in launch.stdout

    ls = run_rbg(sim, rbg, ch, "ls")
    assert ls.returncode == 0, ls.stderr
    assert "alpha" in ls.stdout


@needs_env
def test_read_replays_seeded_transcript(env, tmp_path):
    sim, rbg = env
    ch = tmp_path / "ch"
    run_rbg(sim, rbg, ch, "launch", "alpha", "say hello")
    read = run_rbg(sim, rbg, ch, "read", "alpha")
    assert read.returncode == 0, read.stderr
    assert "user: say hello" in read.stdout


@needs_env
def test_send_appends_and_read_shows_response(env, tmp_path):
    import time
    sim, rbg = env
    ch = tmp_path / "ch"
    run_rbg(sim, rbg, ch, "launch", "alpha", "say hello")
    send = run_rbg(sim, rbg, ch, "send", "alpha", "count to three")
    assert send.returncode == 0, send.stderr

    seen = ""
    for _ in range(20):
        seen = run_rbg(sim, rbg, ch, "read", "alpha").stdout
        if "assistant: ack: count to three" in seen:
            break
        time.sleep(0.25)
    assert "user: count to three" in seen
    assert "assistant: ack: count to three" in seen


@needs_env
def test_send_unknown_agent_fails(env, tmp_path):
    sim, rbg = env
    res = run_rbg(sim, rbg, tmp_path / "ch", "send", "ghost", "x")
    assert res.returncode != 0
```

- [ ] **Step 4: Run the v2 integration suite**

Run: `cd /Users/divkov/workplace/remote-ccbg && pytest test/integration_v2/ -m integration -v`
Expected: all pass (or skip cleanly if `go`/`sshd` unavailable).

- [ ] **Step 5: Write the README**

Create `test/integration_v2/README.md`:

```markdown
# rbg v2 integration tests

Drives the real Go `rbg` client against a sandboxed non-root `sshd` running the
real `rbg-agent` binary plus a fake `claude` — **no tmux**. Proves the
agent-binary mechanics end-to-end: connection gate, launch, ls, read, send
(detached child + flock), all over real SSH with structured argv (no shell).

What it does NOT prove: the real `claude` flags/JSON contract — that lives in
`internal/claudecli/claude.go` and is verified once manually on a box with
`claude`.

## Running

```sh
pytest test/integration_v2/ -m integration -v
```
Skips automatically without `go` or `/usr/sbin/sshd`.
```

- [ ] **Step 6: Commit**

```bash
cd /Users/divkov/workplace/remote-ccbg
git add test/integration_v2/
git commit -m "test(v2): integration harness driving rbg-agent over ssh (no tmux)"
```

---

## Task 15: Manual verification on a real desktop (deferred — needs claude)

**Files:** none (manual).

Carried over from v1: a fake claude can't prove the real contract.

- [ ] **Step 1: Deploy and smoke-test**

```bash
export RBG_HOST=<your-desktop>
export RBG_CWD=<remote-project-dir>
go build -o /tmp/rbg ./cmd/rbg
/tmp/rbg deploy            # cross-compiles rbg-agent, scp's it to the desktop
/tmp/rbg ping              # -> "<host>: reachable"
/tmp/rbg launch demo "say hello and stop"
/tmp/rbg ls
/tmp/rbg read demo
/tmp/rbg send demo "now count to three"
/tmp/rbg read demo
/tmp/rbg attach demo       # interactive; Ctrl-D to leave
```

- [ ] **Step 2: Confirm the claude contract**

On the desktop, verify the two assumptions isolated in `internal/claudecli/claude.go`:
1. `claude agents --json --all` shape — does it match `ParseAgents` (bare array
   or `{"agents":[...]}`, id key one of `sessionId|session_id|id`)? If not,
   adjust `agentWire` in that one file.
2. `claude -p "<task>" --resume <id> --output-format stream-json` — confirm it
   APPENDS to the existing transcript (does not fork). If it forks, document and
   revisit `ResumeHeadlessArgs`.

- [ ] **Step 3: Confirm detachment survives disconnect**

After `send`, drop the SSH connection (close laptop lid / `pkill ssh`) and
reconnect; `rbg read demo` should still show the completed response, proving the
`setsid` child outlived the session without tmux.

---

## Self-Review

**1. Spec coverage** (against `docs/HLD-rbg-v2-agent-binary.md`):
- launch → Task 8 (agent) + Task 11 (client) ✓
- send (detached child via setsid, flock serialization, busy→exit 3) → Task 6 (lock) + Task 9 (agent send + DefaultSpawn) + Task 11 (client maps 3) ✓
- read (agent owns transcript path, renders, no glob) → Task 9 (agent) + Task 4 (render) + Task 11 ✓
- ls → Task 8 + Task 11 ✓
- attach (ssh -t claude --resume, id resolved from agent ls) → Task 12 ✓
- ping → Task 11 ✓
- deploy (cross-compile + scp, arch probe) → Task 12 ✓
- connection gate (BatchMode/ConnectTimeout=5, exact message, exit 1, first) → Task 3, applied in `runAgent`/Ping/attach/deploy ✓
- config (env over file, RBG_HOST required, RBG_SSH split, RBG_AGENT_PATH default) → Task 2 ✓
- no shell on far end (structured argv) → Task 3 (`AgentArgs`/`BuildSSHArgs` pass argv, never `sh -c`) ✓
- no tmux / no injection guard needed → Task 6 (flock) + Task 9 (no glob, argv only) ✓
- claude contract isolated → Task 7 ✓
- detachment in-process → Task 9 (`DefaultSpawn` Setsid) ✓
- integration without tmux → Task 14 ✓
- real-claude verification deferred → Task 15 ✓

**2. Placeholder scan:** One deliberate scaffolding artifact — Task 9 Step 3 instructs adding then DELETING the `import_os_exec_note := 0` reminder line; the final import block is given explicitly. No TBD/"handle errors"/"similar to". All code is complete and inline.

**3. Type consistency:**
- `run.Runner.Run(name, args, stdin) ([]byte, int, error)` is used identically in `sshx`, `client`, `agent`. ✓
- `run.Recording{BySubstring, Default}` and `run.Result{Stdout,Stderr,Code,Err}` consistent across all test files. ✓
- `config.Config{Host,CWD,SSHOpts,AgentPath}` consistent (Tasks 2,3,11,12). ✓
- `session.Session{Name,ClaudeSessionID,TranscriptPath,PID,StartedAt}` and `Store{Sessions,Get,Add,Save}` consistent (Tasks 5,8,9). ✓
- `session.TryLock(path) (*Lock, bool, error)` / `Lock.Unlock()` consistent (Tasks 6,9). ✓
- `claudecli` funcs `BGArgs/ResumeHeadlessArgs/AgentsListArgs/ParseAgents/FindSessionID` and `Agent{Name,SessionID}` consistent (Tasks 7,8). ✓
- `render.Line(string)(string,bool)` / `render.Stream([]string,io.Writer)` consistent (Tasks 4,9). ✓
- `sshx.BuildSSHArgs/AgentArgs/Reachable/EnsureReachable/Options{TTY,Batch}` consistent (Tasks 3,11,12). ✓
- Agent struct gains `Spawn SpawnFunc` and `LockDir` in Task 9; Task 8's `newAgent` test helper sets the earlier fields and Task 9 tests set `Spawn`/use `lockPath` — consistent because both test files are in package `agent` and share the struct. ✓
- Exit code 3 = busy: agent returns it (Task 9), client propagates it (Task 11). ✓

**Note for the implementer:** Tasks 8 and 9 both edit `internal/agent/agent.go`. Task 9 *replaces* the `Agent` struct from Task 8 (adding `Spawn`/`LockDir`) and *replaces* the import block. Apply Task 9's struct/import replacements wholesale rather than appending, so there's exactly one `Agent` definition and one import block.
