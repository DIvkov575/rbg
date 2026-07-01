// Package core is rbg's pure domain layer: one Agent type described by
// orthogonal attributes (where it runs, how far along it is, whether rbg
// started it, its code-sync state), a persisted record Store, a Reconcile
// that merges records with live `claude agents` snapshots into one inventory,
// and lenses (views) over that inventory. The package performs NO I/O beyond
// reading/writing its own JSON store file, so it is fully unit-testable.
package core

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
	"strings"
)

// Location is which machine an agent runs on. The laptop is just one machine,
// so local and remote delegation are the same operation with a different Where.
type Location string

const (
	Local  Location = "local"
	Remote Location = "remote"
)

// Lifecycle is how far along an agent is. Held is "prepared but not yet
// launched" (the old queue/local-agent notion); a held agent always carries a
// real Task — there are no blank placeholders.
type Lifecycle string

const (
	Held    Lifecycle = "held"
	Running Lifecycle = "running"
	Done    Lifecycle = "done"
)

// Origin is whether rbg started the agent. Foreign agents are discovered live
// on a machine but absent from rbg's records; adopting flips them to Managed.
type Origin string

const (
	Managed Origin = "managed"
	Foreign Origin = "foreign"
)

// Sync is the derived git state of an agent's repo/dir. Its value is filled by
// a later layer; SyncUnknown means "not yet determined".
type Sync string

const (
	SyncUnknown Sync = ""
	Aligned     Sync = "aligned"
	Ahead       Sync = "ahead"
	Behind      Sync = "behind"
	Dirty       Sync = "dirty"
)

// Agent is the single unit of delegated work. Local vs remote, held vs running
// vs done, and managed vs foreign are all attributes here, not separate types.
type Agent struct {
	Name    string    `json:"name"`    // stable handle (map key in the Store)
	Repo    string    `json:"repo"`    // git URL / identity; the grouping key
	Dir     string    `json:"dir"`     // working directory on its host
	Task    string    `json:"task"`    // the prompt (held agents still have one)
	Session string    `json:"session"` // claude sessionId once run ("" = never)
	Where   Location  `json:"where"`
	State   Lifecycle `json:"state"`
	Origin  Origin    `json:"origin"`
	Sync    Sync      `json:"sync"`
	RunAt   string    `json:"runAt"` // RFC3339 of last run ("" = never)
	Pid     int       `json:"pid"`   // local child pid for kill (0 for remote; the desktop tracks its own)
}

// IsHeld reports whether the agent is prepared but not yet launched.
func (a Agent) IsHeld() bool { return a.State == Held }

// IsForeign reports whether the agent was started outside rbg.
func (a Agent) IsForeign() bool { return a.Origin == Foreign }

// NewSessionID returns a fresh v4-ish UUID (crypto/rand), formatted 8-4-4-4-12
// and lowercase-hex — glob- and shell-safe. rbg generates the session id BEFORE
// launching claude (claude -p --session-id <id>), so the launched agent's record
// can carry the id and Reconcile can match the record to the live session later.
func NewSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// DeriveSync computes an agent's repo Sync state from observed git facts. The
// priority is deliberate: uncommitted local changes (dirty) are the most
// actionable warning before running a delegated task, so they win over any
// commit divergence; without an upstream, ahead/behind is unknowable so the
// state is SyncUnknown (unless dirty). When an upstream exists and the tree is
// clean: behind (needs a pull before running) outranks ahead, else Aligned.
// This is a single-enum summary: while dirty, ahead/behind divergence is not
// surfaced (resolve the working tree first); callers needing both must re-query.
func DeriveSync(hasUpstream bool, behind, ahead int, dirty bool) Sync {
	switch {
	case dirty:
		return Dirty
	case !hasUpstream:
		return SyncUnknown
	case behind > 0:
		return Behind
	case ahead > 0:
		return Ahead
	default:
		return Aligned
	}
}

// RepoDir resolves a repo identity to the working directory a delegated task
// runs in on its machine. An absolute path (or ~-relative, expanded against
// home) is used verbatim; a bare name or git URL maps to <base>/<repo-leaf>,
// where base is the machine's checkout root (e.g. ~/workplace). This is the
// single place the Repo→Dir convention lives, so Create can stamp Dir at stage
// time and Run never launches a task with an empty working directory. Returns
// "" for an empty repo (a repo-less task keeps whatever Dir it was given).
func RepoDir(base, home, repo string) string {
	if repo == "" {
		return ""
	}
	if strings.HasPrefix(repo, "~/") {
		return filepath.Join(home, repo[2:])
	}
	if filepath.IsAbs(repo) {
		return repo
	}
	leaf := repo
	if i := strings.LastIndexAny(leaf, "/:"); i >= 0 {
		leaf = leaf[i+1:]
	}
	leaf = strings.TrimSuffix(leaf, ".git")
	return filepath.Join(base, leaf)
}

// ValidSessionID reports whether id is a safe session identifier: non-empty and
// matching ^[A-Za-z0-9-]+$. rbg interpolates session ids into glob patterns and
// (for remote transcript reads) into a remote shell command string, so any id
// used there MUST pass this guard first — it admits only characters that are
// inert to globbing and shell parsing, preventing injection.
func ValidSessionID(id string) bool {
	if id == "" {
		return false
	}
	if id[0] == '-' {
		// A leading '-' would be inert to quoting but could be read as a flag by
		// cat/ls in the remote transcript command; real session ids never start
		// with '-', so reject it outright.
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}
