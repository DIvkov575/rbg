package session

import (
	"path/filepath"
	"testing"
)

func TestTryLock_AcquiresAndBlocksSecond(t *testing.T) {
	p := filepath.Join(t.TempDir(), "alpha.lock")
	l1, ok, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("first TryLock should acquire")
	}
	defer l1.Unlock()

	_, ok2, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if ok2 {
		t.Fatal("second TryLock on held lock should fail (busy)")
	}
}

func TestTryLock_ReacquireAfterUnlock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "alpha.lock")
	l1, ok, _ := TryLock(p)
	if !ok {
		t.Fatal("acquire 1")
	}
	l1.Unlock()
	l2, ok2, _ := TryLock(p)
	if !ok2 {
		t.Fatal("should re-acquire after unlock")
	}
	l2.Unlock()
}
