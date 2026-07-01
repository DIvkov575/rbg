package host

import (
	"testing"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/run"
)

func joined(args []string) string {
	s := ""
	for _, a := range args {
		s += a + " "
	}
	return s
}

func TestRemoteRunnerLaunchParsesSession(t *testing.T) {
	cfg := &config.Config{Host: "desktop", Mux: false}
	r := &run.Recording{BySubstring: map[string]run.Result{
		"launch": {Stdout: []byte(`{"id":"fix-bug","claudeSessionId":"sid-42"}`), Code: 0},
	}}
	res, err := RemoteRunner{C: cfg, R: r}.Launch("fix-bug", "fix the bug")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if res.Name != "fix-bug" || res.Session != "sid-42" {
		t.Errorf("RunResult = %+v, want {fix-bug sid-42}", res)
	}
	// It must go over ssh, invoking the rbg-agent launch verb with the task.
	if len(r.Calls) != 1 || r.Calls[0].Name != "ssh" {
		t.Fatalf("expected one ssh call, got %+v", r.Calls)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "launch") || !contains(j, "fix the bug") || !contains(j, "desktop") {
		t.Errorf("ssh args missing launch/task/host: %v", r.Calls[0].Args)
	}
}

func TestRemoteRunnerLaunchNonZeroErrors(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte("boom"), Code: 1}}
	if _, err := (RemoteRunner{C: cfg, R: r}).Launch("n", "t"); err == nil {
		t.Errorf("expected error on non-zero launch exit")
	}
}

func TestRemoteRunnerSendBusyIsError(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 3}} // rbg-agent busy signal
	err := RemoteRunner{C: cfg, R: r}.Send("fix-bug", "more")
	if err == nil {
		t.Fatalf("expected busy error on exit 3")
	}
	if err != ErrBusy {
		t.Errorf("err = %v, want ErrBusy", err)
	}
}

func TestRemoteRunnerSendOK(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Code: 0}}
	if err := (RemoteRunner{C: cfg, R: r}).Send("fix-bug", "more"); err != nil {
		t.Errorf("Send ok: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "send") || !contains(j, "more") {
		t.Errorf("ssh args missing send/task: %v", r.Calls[0].Args)
	}
}

func TestRemoteRunnerKill(t *testing.T) {
	cfg := &config.Config{Host: "desktop"}
	r := &run.Recording{Default: run.Result{Stdout: []byte(`{"ok":"killed","id":"fix-bug"}`), Code: 0}}
	if err := (RemoteRunner{C: cfg, R: r}).Kill("fix-bug"); err != nil {
		t.Errorf("Kill: %v", err)
	}
	j := joined(r.Calls[0].Args)
	if !contains(j, "kill") {
		t.Errorf("ssh args missing kill: %v", r.Calls[0].Args)
	}
}
