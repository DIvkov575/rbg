// Package dbg is rbg's opt-in debug log. It is OFF by default and enabled by
// setting RBG_DEBUG to a non-empty value. Output goes to a FILE
// (~/.rbg/debug.log, or $RBG_DEBUG if it looks like a path) rather than stderr,
// so it never corrupts the interactive dashboard's screen. Every subprocess rbg
// runs (ssh/claude/git) is logged through here, which is how you diagnose remote
// failures like an ssh connection timeout.
package dbg

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu      sync.Mutex
	enabled bool
	path    string
	inited  bool
)

// now is overridable in tests (the real clock is used in production).
var now = time.Now

// Enabled reports whether debug logging is on (RBG_DEBUG set to non-empty).
func Enabled() bool {
	initOnce()
	return enabled
}

// Path returns the debug log file path (empty when disabled).
func Path() string {
	initOnce()
	return path
}

// initOnce resolves RBG_DEBUG once: empty → off; "1"/"true"/etc → default path
// (~/.rbg/debug.log); anything containing a slash → that exact file path.
func initOnce() {
	mu.Lock()
	defer mu.Unlock()
	if inited {
		return
	}
	inited = true
	v := os.Getenv("RBG_DEBUG")
	if v == "" {
		return
	}
	enabled = true
	if filepath.IsAbs(v) || filepath.Base(v) != v {
		path = v // an explicit path
		return
	}
	home, _ := os.UserHomeDir()
	path = filepath.Join(home, ".rbg", "debug.log")
}

// Logf appends a timestamped line to the debug log when enabled; a no-op
// otherwise. Logging failures are swallowed — debug must never break a command.
func Logf(format string, args ...any) {
	if !Enabled() {
		return
	}
	line := now().UTC().Format("15:04:05.000") + " " + fmt.Sprintf(format, args...) + "\n"
	mu.Lock()
	p := path
	mu.Unlock()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// reset is a test hook to re-evaluate RBG_DEBUG.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	inited, enabled, path = false, false, ""
}
