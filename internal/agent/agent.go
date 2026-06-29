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
	StatePath  string        // ~/.rbg-agent/sessions.json
	ClaudeHome string        // root for transcript paths (~ in prod)
	Now        func() string // timestamp source (injectable for tests)
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
