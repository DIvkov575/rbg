// Package claudecli isolates the contract with the real `claude` binary: the
// argv we pass to it. EVERYTHING rbg assumes about how claude is invoked lives
// here, so the one-time real-host verification has a single place to land.
//
// As verified against the ASBX claude (v2.1.187) on a real desktop, rbg drives
// claude entirely headlessly with client-generated session ids:
//   - launch: claude -p <task> --session-id <uuid>   (no --bg → no resident
//     process holding a session lock)
//   - send:   claude -p <task> --resume <uuid>        (appends to the session;
//     does not fork)
//
// rbg reads results from the on-disk transcript (located by session-id glob),
// never from claude's stdout, so no --output-format flag is needed (and
// --output-format stream-json is in fact rejected by this distribution).
//
// Both invocations pass --dangerously-skip-permissions. A headless `-p` run has
// no interactive stdin (the detached child's stdin is /dev/null), so any
// tool-permission prompt would block forever — the run appears to "buffer" and
// never executes the task. Skipping permission prompts lets the prompt trigger
// claude's work immediately. The agents run on the user's own trusted desktop.
package claudecli

// skipPerms is appended to every headless invocation; see the package note.
const skipPerms = "--dangerously-skip-permissions"

// LaunchHeadlessArgs builds `claude -p <task> --session-id <uuid>`. The
// client-chosen id means the agent never needs `claude agents --json` to
// discover it, and the plain -p run leaves no resident background process, so
// subsequent ResumeHeadlessArgs calls append cleanly.
func LaunchHeadlessArgs(sessionID, task string) []string {
	return []string{"-p", task, "--session-id", sessionID, skipPerms}
}

// ResumeHeadlessArgs builds `claude -p <task> --resume <uuid>`, the headless
// send invocation. It appends to the existing transcript (does not fork). We
// deliberately omit --output-format stream-json: that flag combination fails
// with exit 1 on the ASBX distribution, and results are read from the
// transcript file rather than the child's stdout.
func ResumeHeadlessArgs(sessionID, task string) []string {
	return []string{"-p", task, "--resume", sessionID, skipPerms}
}

// AgentsListArgs builds `claude agents --json --all`, the headless listing of
// every background session on a host regardless of spawner (verified against
// claude v2.1.197). --json prints a JSON array and exits without a TTY; --all
// includes completed sessions, so rbg sees finished agents too. The result is
// decoded into []core.Live by the host layer. No permission flag is needed:
// listing runs no tools.
func AgentsListArgs() []string {
	return []string{"agents", "--json", "--all"}
}
