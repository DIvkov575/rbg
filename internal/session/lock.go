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
//
// The lock file itself is intentionally NOT removed on Unlock: deleting a
// flock'd path is racy (another process may hold or be about to open the same
// path, and unlinking it detaches their lock from ours), so we leave the
// zero-byte file in place and re-lock the same inode next time. Callers that
// point LockDir at a persistent dir accumulate one small file per distinct id;
// point it at a temp dir if that matters.
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
