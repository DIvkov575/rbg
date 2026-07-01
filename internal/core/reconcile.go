package core

import "sort"

// Reconcile merges rbg's persisted records with the live agents observed on the
// local and remote machines into one inventory. Matching is by Session id,
// which is stable across hosts and refreshes:
//
//   - A record with a Session that appears live keeps its identity (Name, Repo,
//     Task) but takes State from the live snapshot and Where from which host
//     reported it. It stays Managed.
//   - A record whose Session is empty (a Held agent) or does not appear live is
//     passed through unchanged — it is still a real, managed unit of work.
//   - A live agent with no matching record is Foreign: Where from the host it was
//     seen on, Name/Dir/State/Session from the live entry.
//
// The result is sorted by Name for a stable display order.
func Reconcile(records []Agent, localLive, remoteLive []Live) []Agent {
	// Index records by Session so live agents can find their record. Empty
	// sessions are never indexed, so a Held record never matches a live entry.
	bySession := map[string]int{} // session id -> index into out
	var out []Agent

	for _, rec := range records {
		idx := len(out)
		out = append(out, rec)
		if rec.Session != "" {
			bySession[rec.Session] = idx
		}
	}

	merge := func(live []Live, where Location) {
		for _, lv := range live {
			if lv.SessionID != "" {
				if idx, ok := bySession[lv.SessionID]; ok {
					// Managed agent that has run: refresh live-derived fields.
					out[idx].Where = where
					out[idx].State = LifecycleFromState(lv.State)
					if out[idx].Dir == "" {
						out[idx].Dir = lv.Cwd
					}
					continue
				}
			}
			// No record: a foreign agent discovered on this host.
			out = append(out, Agent{
				Name:    lv.Name,
				Dir:     lv.Cwd,
				Session: lv.SessionID,
				Where:   where,
				State:   LifecycleFromState(lv.State),
				Origin:  Foreign,
			})
		}
	}
	merge(localLive, Local)
	merge(remoteLive, Remote)

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Adopt takes a foreign agent under rbg management by flipping its Origin to
// Managed, leaving all other fields untouched. The transform is pure and
// idempotent; persisting the result is the caller's job (a later layer).
func Adopt(a Agent) Agent {
	a.Origin = Managed
	return a
}
