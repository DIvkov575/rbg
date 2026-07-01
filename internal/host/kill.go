package host

import (
	"fmt"
	"syscall"
)

// KillProcessGroup sends SIGTERM to the process GROUP led by pid. rbg's local
// children are spawned in their own process group (agent.DefaultSpawn sets
// Setpgid), so signalling the negative pid terminates the child and any
// grandchildren it started. pid must be positive.
func KillProcessGroup(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	return syscall.Kill(-pid, syscall.SIGTERM)
}
