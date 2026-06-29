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
