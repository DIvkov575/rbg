package host

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/divkov575/rbg/internal/core"
)

// Transcripts reads an agent's raw .jsonl conversation transcript from one
// machine, located by session id (the on-disk cwd-slug dir is unpredictable).
type Transcripts interface {
	// Read returns the raw transcript bytes for a claude session id.
	Read(session string) ([]byte, error)
}

// transcriptGlob is the session-id glob under a home dir, matching claude's
// layout ~/.claude/projects/<cwd-slug>/<sessionId>.jsonl.
func transcriptGlob(home, session string) string {
	return filepath.Join(home, ".claude", "projects", "*", session+".jsonl")
}

// LocalTranscripts reads transcripts from the laptop's ~/.claude tree. Home is
// the home directory root (injectable so tests use t.TempDir()).
type LocalTranscripts struct {
	Home string
}

// Read globs the local claude tree for the session's transcript and returns it.
func (l LocalTranscripts) Read(session string) ([]byte, error) {
	if !core.ValidSessionID(session) {
		return nil, fmt.Errorf("invalid session id %q", session)
	}
	matches, err := filepath.Glob(transcriptGlob(l.Home, session))
	if err != nil {
		return nil, fmt.Errorf("glob transcript: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no transcript found for session %s", session)
	}
	return os.ReadFile(matches[0])
}

// SaveMirror writes pulled transcript bytes to the laptop's rbg-owned mirror
// (<home>/.rbg/transcripts/<session>.jsonl) so a remote transcript has a stable
// local home, and returns the path. The session id is guarded before use in the
// path.
func SaveMirror(home, session string, data []byte) (string, error) {
	if !core.ValidSessionID(session) {
		return "", fmt.Errorf("invalid session id %q", session)
	}
	dir := filepath.Join(home, ".rbg", "transcripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, session+".jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

var _ Transcripts = LocalTranscripts{}
