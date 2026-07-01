package cli

import (
	"fmt"
	"strings"

	"github.com/divkov575/rbg/internal/core"
)

// renderAgents formats the inventory as a compact, aligned table: one row per
// agent showing name, where it runs, its state, origin, and repo. Foreign
// agents are marked so the operator can tell them from managed ones.
func renderAgents(agents []core.Agent) string {
	if len(agents) == 0 {
		return "no agents\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-20s  %-6s  %-8s  %-8s  %s\n", "NAME", "WHERE", "STATE", "ORIGIN", "REPO")
	for _, a := range agents {
		fmt.Fprintf(&b, "%-20s  %-6s  %-8s  %-8s  %s\n",
			a.Name, a.Where, a.State, a.Origin, a.Repo)
	}
	return b.String()
}
