package host

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestKillProcessGroupStopsAChild(t *testing.T) {
	// Start a real, long-sleeping child in its own process group, then kill it.
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // child leads its own group
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	if err := KillProcessGroup(cmd.Process.Pid); err != nil {
		t.Fatalf("KillProcessGroup: %v", err)
	}
	select {
	case <-done: // child exited (killed) — success
	case <-time.After(5 * time.Second):
		t.Fatalf("child was not killed within 5s")
	}
}

func TestKillProcessGroupInvalidPidErrors(t *testing.T) {
	// pid 0 / negative are not valid targets for this helper.
	if err := KillProcessGroup(0); err == nil {
		t.Errorf("expected error for pid 0")
	}
}
