package host

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
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

// RemoteTranscripts reads transcripts from the desktop over SSH. It runs
// `sh -c 'cat <glob>'` so the DESKTOP shell expands the session-id glob (the
// cwd-slug dir is unknown to the laptop). The session id is validated before it
// is placed into that command string, which is the shell-injection defense.
type RemoteTranscripts struct {
	C *config.Config
	R run.Runner
}

// Read cats the desktop transcript for the session and returns its bytes.
func (s RemoteTranscripts) Read(session string) ([]byte, error) {
	if !core.ValidSessionID(session) {
		return nil, fmt.Errorf("invalid session id %q", session)
	}
	// The glob uses ~ so the desktop shell resolves the remote home. sshx quotes
	// each remote token, so the login shell hands `sh -c` this exact command and
	// the inner sh expands the glob. session is guarded above, so it is inert.
	catCmd := "cat ~/.claude/projects/*/" + session + ".jsonl"
	remote := []string{"sh", "-c", catCmd}
	sshArgs := sshx.BuildSSHArgs(s.C, remote, sshx.Options{ConnectTimeout: true})
	out, code, err := s.R.Run("ssh", sshArgs, nil)
	if err != nil {
		return nil, fmt.Errorf("remote transcript read: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("remote transcript read exited %d: %s", code, out)
	}
	return out, nil
}

var _ Transcripts = RemoteTranscripts{}
