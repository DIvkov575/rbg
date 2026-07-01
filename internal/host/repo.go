package host

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
)

// Repo reconciles and reports the git state of a checkout on one machine.
type Repo interface {
	// Status reports the checkout's Sync state (aligned/ahead/behind/dirty/unknown).
	Status(dir string) (core.Sync, error)
	// Pull fast-forwards the checkout, so a delegated task runs against upstream.
	Pull(dir string) error
}

// gitRunner runs one git subcommand in dir and returns stdout + exit code. The
// dir is applied via `git -C <dir>` (no shell cd), so local and remote share it.
type gitRunner func(dir string, args []string) ([]byte, int, error)

// syncStatus gathers the three git facts (dirty, has-upstream, behind/ahead)
// via g and derives the Sync state. A hard failure of the dirty/upstream probes
// is an error; a non-zero upstream probe simply means "no upstream" (not an
// error), and rev-list is skipped in that case.
func syncStatus(g gitRunner, dir string) (core.Sync, error) {
	// dirty?
	out, code, err := g(dir, []string{"status", "--porcelain"})
	if err != nil {
		return core.SyncUnknown, fmt.Errorf("git status: %w", err)
	}
	if code != 0 {
		return core.SyncUnknown, fmt.Errorf("git status exited %d: %s", code, out)
	}
	dirty := len(strings.TrimSpace(string(out))) > 0

	// has upstream? (non-zero exit = no upstream configured, not a failure)
	_, code, err = g(dir, []string{"rev-parse", "--abbrev-ref", "@{u}"})
	if err != nil {
		return core.SyncUnknown, fmt.Errorf("git rev-parse: %w", err)
	}
	hasUpstream := code == 0

	var behind, ahead int
	if hasUpstream {
		out, code, err = g(dir, []string{"rev-list", "--left-right", "--count", "@{u}...HEAD"})
		if err != nil {
			return core.SyncUnknown, fmt.Errorf("git rev-list: %w", err)
		}
		if code != 0 {
			return core.SyncUnknown, fmt.Errorf("git rev-list exited %d: %s", code, out)
		}
		behind, ahead, err = parseAheadBehind(out)
		if err != nil {
			return core.SyncUnknown, err
		}
	}
	return core.DeriveSync(hasUpstream, behind, ahead, dirty), nil
}

// parseAheadBehind parses `git rev-list --left-right --count` output, two
// tab/space-separated ints: <behind>\t<ahead> (left=upstream-only, right=HEAD-only).
func parseAheadBehind(out []byte) (behind, ahead int, err error) {
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output %q", string(out))
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse behind %q: %w", fields[0], err)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse ahead %q: %w", fields[1], err)
	}
	return behind, ahead, nil
}

// pull fast-forwards the checkout via g.
func pull(g gitRunner, dir string) error {
	out, code, err := g(dir, []string{"pull", "--ff-only"})
	if err != nil {
		return fmt.Errorf("git pull: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("git pull exited %d: %s", code, out)
	}
	return nil
}

// LocalRepo runs git on the laptop.
type LocalRepo struct {
	R run.Runner
}

// git runs `git -C <dir> <args>` locally.
func (l LocalRepo) git(dir string, args []string) ([]byte, int, error) {
	return l.R.Run("git", append([]string{"-C", dir}, args...), nil)
}

func (l LocalRepo) Status(dir string) (core.Sync, error) { return syncStatus(l.git, dir) }
func (l LocalRepo) Pull(dir string) error                { return pull(l.git, dir) }

var _ Repo = LocalRepo{}
