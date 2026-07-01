package host

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

// writeTranscript plants a transcript file under home's claude tree and returns
// nothing; it mimics claude's real layout: ~/.claude/projects/<slug>/<sid>.jsonl.
func writeTranscript(t *testing.T, home, slug, sid, content string) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalTranscriptsReadFindsBySessionGlob(t *testing.T) {
	home := t.TempDir()
	sid := "55a63641-2b5e-413e-bd07-00a74bbc1dfc"
	writeTranscript(t, home, "-some-unpredictable-cwd-slug", sid, `{"line":1}`+"\n")

	data, err := LocalTranscripts{Home: home}.Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != `{"line":1}`+"\n" {
		t.Errorf("Read = %q, want the transcript content", data)
	}
}

func TestLocalTranscriptsReadMissingIsError(t *testing.T) {
	home := t.TempDir()
	_, err := LocalTranscripts{Home: home}.Read("11111111-2222-3333-4444-555555555555")
	if err == nil {
		t.Errorf("expected error reading a nonexistent transcript")
	}
}

func TestLocalTranscriptsReadRejectsBadSessionID(t *testing.T) {
	home := t.TempDir()
	_, err := LocalTranscripts{Home: home}.Read("../etc/passwd")
	if err == nil {
		t.Errorf("expected error for an invalid session id (guard)")
	}
}

func TestSaveMirrorWritesToRbgDir(t *testing.T) {
	home := t.TempDir()
	sid := "55a63641-2b5e-413e-bd07-00a74bbc1dfc"
	content := []byte(`{"mirrored":true}` + "\n")

	path, err := SaveMirror(home, sid, content)
	if err != nil {
		t.Fatalf("SaveMirror: %v", err)
	}
	want := filepath.Join(home, ".rbg", "transcripts", sid+".jsonl")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading mirrored file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("mirrored content = %q, want %q", got, content)
	}
}

func TestSaveMirrorRejectsBadSessionID(t *testing.T) {
	if _, err := SaveMirror(t.TempDir(), "bad/../id", []byte("x")); err == nil {
		t.Errorf("expected error for invalid session id")
	}
}

func TestRemoteTranscriptsReadCatsOverSSH(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	sid := "55a63641-2b5e-413e-bd07-00a74bbc1dfc"
	r := &run.Recording{Default: run.Result{Stdout: []byte(`{"remote":1}` + "\n"), Code: 0}}

	data, err := RemoteTranscripts{C: cfg, R: r}.Read(sid)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != `{"remote":1}`+"\n" {
		t.Errorf("Read = %q", data)
	}
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("expected one ssh call, got %+v", r.Calls)
	}
	j := joined(r.Calls[0].Args)
	// must invoke a shell to expand the glob, carry the session id and host.
	if !contains(j, "sh") || !contains(j, sid) || !contains(j, "desktop") {
		t.Errorf("ssh args missing sh/sid/host: %v", r.Calls[0].Args)
	}
	if !contains(j, "projects") {
		t.Errorf("ssh args missing the claude projects glob: %v", r.Calls[0].Args)
	}
}

func TestRemoteTranscriptsReadRejectsBadSessionID(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 0}}
	if _, err := (RemoteTranscripts{C: cfg, R: r}).Read("evil; rm -rf ~"); err == nil {
		t.Errorf("expected invalid-session-id error BEFORE any ssh call")
	}
	if len(r.Calls) != 0 {
		t.Errorf("must not run ssh for an invalid session id, got %+v", r.Calls)
	}
}

func TestRemoteTranscriptsReadNonZeroErrors(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte("cat: no such file"), Code: 1}}
	if _, err := (RemoteTranscripts{C: cfg, R: r}).Read("55a63641-2b5e-413e-bd07-00a74bbc1dfc"); err == nil {
		t.Errorf("expected error on non-zero cat exit")
	}
}
