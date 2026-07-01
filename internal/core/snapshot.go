package core

// Live is one element of `claude agents --json --all` (verified shape, claude
// v2.1.197): keys id, cwd, kind, sessionId, name, state, startedAt, and
// optionally pid/status. startedAt is Unix-milliseconds. We decode only the
// fields reconcile needs.
type Live struct {
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	State     string `json:"state"`
	StartedAt int64  `json:"startedAt"`
}

// LifecycleFromState maps claude's live state string onto our Lifecycle. A live
// agent is never Held (Held means "not yet launched", which by definition never
// appears in `claude agents`): "working"/"idle" are Running, everything else
// (including "done", unknown, and empty) is Done.
func LifecycleFromState(state string) Lifecycle {
	switch state {
	case "working", "idle":
		return Running
	default:
		return Done
	}
}
