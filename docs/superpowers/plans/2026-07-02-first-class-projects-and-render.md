# First-Class Projects + Own-Render Transcript Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a project a first-class, directory-anchored entity that owns agents and auto-detects its git origin (mirrored to the desktop on demand), and replace the three incoherent "open an agent" paths with one faithful, tailing transcript render — follow-ups delivered by resume (done) or daemon-socket injection (live).

**Architecture:** Pure `core` gains a `Project` type + `ProjectStore` + auto-adopt reconcile; `engine` gains project CRUD + lazy remote mirror and stamps `Agent.ProjectDir` on spawn; `render` is rewritten to a rich turn-oriented reconstruction; `ptybridge` keeps only codec+roster+new `Inject`; `uitea` makes the project view primary with add/rename keys and feeds the pager from the new renderer with a live tail.

**Tech Stack:** Go 1.26, module `github.com/divkov575/rbg`. Build/test ALWAYS with `GOPROXY=off GOFLAGS=-mod=vendor`. Deps vendored (bubbletea, lipgloss, x/term). Commit with `git -c user.email="dmitriy@ivkov.net"`.

**Spec:** `docs/superpowers/specs/2026-07-02-first-class-projects-and-render-design.md`

---

## File Structure

**Slice A — core project model**
- Modify `internal/core/project.go` — rename suggestion type `Project`→`Suggestion`, `MergeProjects`→`MergeSuggestions`; add first-class `Project{Dir,Name,Repo,Remote}`.
- Modify `internal/core/agent.go` — add `Agent.ProjectDir`.
- Modify `internal/core/lenses.go` — `ProjectKey` uses `ProjectDir`; `GroupByProject` takes projects, renders empty projects.
- Create `internal/core/projectstore.go` — `ProjectStore` over `projects.json`.
- Create `internal/core/reconcile_projects.go` — `ReconcileProjects`.

**Slice B — host suggestion + mirror plumbing**
- Modify `internal/host/projects.go` — return `core.Suggestion` (mechanical rename).
- Modify `internal/agent/agent.go` — `Clone` accepts an explicit dest dir.
- Modify `cmd/rbg-agent/main.go` — `clone --dir`; add `inject --id --text` verb.

**Slice C — engine projects + mirror + spawn wiring**
- Modify `internal/engine/engine.go` — add `projectStore`, `suggestions` closure; keep `Suggestions()`.
- Modify `internal/engine/ops.go` — real `Projects()`, `Suggestions()`, `AddProject`, `RenameProject`.
- Create `internal/engine/projects.go` — `AddProject`/`RenameProject`/`MirrorProject`/`detectOrigin`.
- Modify `internal/engine/ops.go` `Create` — stamp `ProjectDir`; remote spawn mirrors first.

**Slice D — render rewrite**
- Modify `internal/render/render.go` — `Render(jsonl, opts) []string` turn-oriented; drop `Line`/`Stream`.
- Modify `internal/render/render_test.go` — golden tests.

**Slice E — ptybridge trim + inject**
- Delete `internal/ptybridge/bridge.go`, `attach.go`, `bridge_test.go`.
- Create `internal/ptybridge/inject.go` + `inject_test.go`.
- Modify `cmd/rbg/main.go` — remove `attach`; (inject reached via rbg-agent).

**Slice F — engine send routing (done vs live)**
- Modify `internal/engine/control.go` `Send` — route live→inject, done→resume.

**Slice G — uitea rewire**
- Modify `internal/uitea/uitea.go` — `Ops` gains project methods; pager fed by `render.Render`.
- Modify `internal/uitea/pager.go` — rich render + tail.
- Modify `internal/uitea/update.go` — tail tick; open one render path; `^n` add project, `^e` rename.
- Modify `internal/uitea/view.go` — project view primary (name/repo/mirror badges, empty projects).
- Modify `internal/uitea/picker.go` — dir-picker for add-project (uses `Suggestion`).
- Delete `openClientCmd`.

---

## Slice A — core project model

### Task A1: Rename suggestion type; add first-class Project

**Files:**
- Modify: `internal/core/project.go`

- [ ] **Step 1: Rewrite project.go**

```go
package core

import "sort"

// Suggestion is a pickable target when ADDING a project — a repo/dir the user is
// likely to want. It unifies local checkouts, remote checkouts, and GitHub repos
// behind one value: Repo is what add would receive; Label is what the picker shows.
// (Formerly named Project; renamed when Project became a first-class stored type.)
type Suggestion struct {
	Label  string // human label shown in the picker
	Repo   string // the value passed to add (path or git URL/name)
	Origin string // "local" | "remote" | "github" | "agent"
}

// MergeSuggestions unifies suggestions from several sources into one sorted,
// de-duplicated list. Dedup is by Repo; first occurrence wins, so callers pass
// the most specific source first. Sorted by Label for stable display.
func MergeSuggestions(lists ...[]Suggestion) []Suggestion {
	seen := map[string]bool{}
	var out []Suggestion
	for _, list := range lists {
		for _, p := range list {
			if p.Repo == "" || seen[p.Repo] {
				continue
			}
			seen[p.Repo] = true
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// Project is a first-class unit of work: a local directory that owns agents and
// has at most one auto-detected git repo. Identity is Dir (the ProjectStore map
// key); it exists independently of any agent.
type Project struct {
	Dir    string `json:"dir"`    // absolute LOCAL dir — identity / store key
	Name   string `json:"name"`   // display name; defaults to leaf(Dir), renameable
	Repo   string `json:"repo"`   // origin URL, auto-detected; "" if not a git repo
	Remote string `json:"remote"` // mirrored desktop dir; "" until mirrored
}
```

- [ ] **Step 2: Build core (expect breakage elsewhere; core itself compiles)**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go build ./internal/core/`
Expected: PASS (core has no internal user of the old names except lenses, fixed next task).

- [ ] **Step 3: Commit**

```bash
git add internal/core/project.go
git -c user.email="dmitriy@ivkov.net" commit -m "refactor(core): rename Project suggestion type to Suggestion; add first-class Project"
```

### Task A2: Agent.ProjectDir + link-based grouping

**Files:**
- Modify: `internal/core/agent.go`
- Modify: `internal/core/lenses.go`
- Test: `internal/core/lenses_test.go`

- [ ] **Step 1: Add ProjectDir to Agent**

In `internal/core/agent.go`, inside `type Agent struct`, after the `Dir` field add:

```go
	ProjectDir string    `json:"projectDir"` // owning project's Dir ("" = fall back to Dir)
```

- [ ] **Step 2: Write failing test for link-based grouping**

Add to `internal/core/lenses_test.go`:

```go
func TestGroupByProjectUsesLinkAndKeepsEmpty(t *testing.T) {
	agents := []Agent{
		{Name: "a", ProjectDir: "/w/app", Dir: "/w/app"},
		{Name: "b", Dir: "/w/lib"}, // no ProjectDir → falls back to Dir
	}
	projects := []Project{
		{Dir: "/w/app", Name: "app"},
		{Dir: "/w/empty", Name: "empty"}, // zero agents, must still appear
	}
	groups := GroupByProject(agents, projects)
	byName := map[string]int{}
	for _, g := range groups {
		byName[g.Project] = len(g.Agents)
	}
	if byName["/w/app"] != 1 {
		t.Errorf("app group = %d, want 1", byName["/w/app"])
	}
	if _, ok := byName["/w/empty"]; !ok {
		t.Error("empty project must render with zero agents")
	}
	if byName["/w/lib"] != 1 {
		t.Errorf("unlisted dir /w/lib should still group by fallback, got %d", byName["/w/lib"])
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/core/ -run TestGroupByProjectUsesLinkAndKeepsEmpty`
Expected: FAIL (compile error: `GroupByProject` takes 1 arg).

- [ ] **Step 4: Rewrite ProjectKey + GroupByProject in lenses.go**

Replace `ProjectKey` and `GroupByProject` in `internal/core/lenses.go` with:

```go
// ProjectKey is which project an agent belongs to: its explicit ProjectDir link
// if set, else its own Dir, else its Repo leaf, else "" (the unlinked bucket).
func ProjectKey(a Agent) string {
	if a.ProjectDir != "" {
		return a.ProjectDir
	}
	if a.Dir != "" {
		return a.Dir
	}
	if a.Repo != "" {
		return leaf(a.Repo)
	}
	return ""
}

// GroupByProject groups agents under their ProjectKey. It takes the known
// projects so a project with zero agents still renders. Group keys are project
// Dirs (or fallback keys); groups sort by key with the unlinked "" bucket last;
// agents within a group sort by Name.
func GroupByProject(agents []Agent, projects []Project) []ProjectGroup {
	byKey := map[string][]Agent{}
	for _, a := range agents {
		byKey[ProjectKey(a)] = append(byKey[ProjectKey(a)], a)
	}
	// ensure every known project appears, even with no agents.
	for _, p := range projects {
		if _, ok := byKey[p.Dir]; !ok {
			byKey[p.Dir] = nil
		}
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if (keys[i] == "") != (keys[j] == "") {
			return keys[j] == ""
		}
		return keys[i] < keys[j]
	})
	out := make([]ProjectGroup, 0, len(keys))
	for _, k := range keys {
		members := byKey[k]
		sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
		out = append(out, ProjectGroup{Project: k, Agents: members})
	}
	return out
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/core/ -run TestGroupByProjectUsesLinkAndKeepsEmpty`
Expected: PASS. (Other core tests referencing `GroupByProject(x)` with one arg will now fail to compile — fix those call sites to pass `nil` for the projects arg in the same commit.)

- [ ] **Step 6: Fix existing one-arg GroupByProject callers in tests**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/core/ 2>&1 | grep GroupByProject` — for each failing call in `internal/core/*_test.go`, change `GroupByProject(agents)` → `GroupByProject(agents, nil)`.

- [ ] **Step 7: Commit**

```bash
git add internal/core/agent.go internal/core/lenses.go internal/core/lenses_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(core): explicit Agent.ProjectDir link; GroupByProject renders empty projects"
```

### Task A3: ProjectStore

**Files:**
- Create: `internal/core/projectstore.go`
- Test: `internal/core/projectstore_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/core/projectstore_test.go`:

```go
package core

import (
	"path/filepath"
	"testing"
)

func TestProjectStoreRoundTripRenameDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")
	s, err := LoadProjectStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Add(Project{Dir: "/w/app", Name: "app", Repo: "git@x:me/app.git"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := LoadProjectStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := s2.Get("/w/app"); !ok || p.Repo != "git@x:me/app.git" {
		t.Fatalf("reload lost project: %+v ok=%v", p, ok)
	}
	if !s2.Rename("/w/app", "renamed") {
		t.Fatal("rename should succeed")
	}
	if p, _ := s2.Get("/w/app"); p.Name != "renamed" {
		t.Errorf("name = %q, want renamed", p.Name)
	}
	if s2.Rename("/w/missing", "x") {
		t.Error("rename of missing dir should be false")
	}
	s2.Delete("/w/app")
	if _, ok := s2.Get("/w/app"); ok {
		t.Error("delete failed")
	}
}

func TestLoadProjectStoreMissingIsEmpty(t *testing.T) {
	s, err := LoadProjectStore(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(s.Records()) != 0 {
		t.Fatalf("missing file should be empty store, err=%v n=%d", err, len(s.Records()))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/core/ -run TestProjectStore`
Expected: FAIL (LoadProjectStore undefined).

- [ ] **Step 3: Implement projectstore.go**

Create `internal/core/projectstore.go` (mirror `store.go` exactly):

```go
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// ProjectStore is rbg's on-disk registry of first-class projects at
// ~/.rbg/projects.json, keyed by absolute Dir. Same atomic-write, corrupt→empty
// semantics as Store.
type ProjectStore struct {
	path     string
	projects map[string]Project
}

func LoadProjectStore(path string) (*ProjectStore, error) {
	s := &ProjectStore{path: path, projects: map[string]Project{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var wrap struct {
		Projects map[string]Project `json:"projects"`
	}
	_ = json.Unmarshal(data, &wrap)
	if wrap.Projects != nil {
		s.projects = wrap.Projects
	}
	return s, nil
}

func (s *ProjectStore) Add(p Project) { s.projects[p.Dir] = p }

func (s *ProjectStore) Get(dir string) (Project, bool) { p, ok := s.projects[dir]; return p, ok }

// Rename sets the display name for dir; returns false if dir is absent.
func (s *ProjectStore) Rename(dir, name string) bool {
	p, ok := s.projects[dir]
	if !ok {
		return false
	}
	p.Name = name
	s.projects[dir] = p
	return true
}

func (s *ProjectStore) Delete(dir string) { delete(s.projects, dir) }

func (s *ProjectStore) Records() []Project {
	out := make([]Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *ProjectStore) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Projects map[string]Project `json:"projects"`
	}{s.projects}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/core/ -run TestProjectStore`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/projectstore.go internal/core/projectstore_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(core): ProjectStore over projects.json (add/get/rename/delete/save)"
```

### Task A4: ReconcileProjects auto-adopt

**Files:**
- Create: `internal/core/reconcile_projects.go`
- Test: `internal/core/reconcile_projects_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/core/reconcile_projects_test.go`:

```go
package core

import "testing"

func TestReconcileProjectsAutoAdopts(t *testing.T) {
	stored := []Project{{Dir: "/w/app", Name: "app", Repo: "r"}}
	agents := []Agent{
		{Name: "a", ProjectDir: "/w/app"},     // already stored
		{Name: "b", ProjectDir: "/w/adopted"}, // new → auto-adopt
		{Name: "c", Dir: "/w/byDir"},          // no ProjectDir → adopt by Dir
		{Name: "d"},                           // no dir at all → no project
	}
	got := ReconcileProjects(stored, agents)
	byDir := map[string]Project{}
	for _, p := range got {
		byDir[p.Dir] = p
	}
	if byDir["/w/app"].Repo != "r" {
		t.Error("stored project should be preserved with its repo")
	}
	if byDir["/w/adopted"].Name != "adopted" {
		t.Errorf("adopted name = %q, want leaf 'adopted'", byDir["/w/adopted"].Name)
	}
	if _, ok := byDir["/w/byDir"]; !ok {
		t.Error("agent with only Dir should be adopted")
	}
	if len(got) != 3 {
		t.Errorf("want 3 projects, got %d (%v)", len(got), byDir)
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/core/ -run TestReconcileProjectsAutoAdopts`
Expected: FAIL (ReconcileProjects undefined).

- [ ] **Step 3: Implement reconcile_projects.go**

Create `internal/core/reconcile_projects.go`:

```go
package core

import "sort"

// ReconcileProjects returns the full project list to display: every stored
// project, plus a synthesized project for any agent whose ProjectKey names a dir
// not already stored (auto-adopt), so dirs-with-agents always appear. Synthesized
// projects get Name=leaf(dir) and empty Repo (origin detection is I/O, done by
// the engine). Deduped by Dir, sorted by Name.
func ReconcileProjects(stored []Project, agents []Agent) []Project {
	byDir := map[string]Project{}
	for _, p := range stored {
		byDir[p.Dir] = p
	}
	for _, a := range agents {
		k := ProjectKey(a)
		if k == "" {
			continue
		}
		if _, ok := byDir[k]; !ok {
			byDir[k] = Project{Dir: k, Name: leaf(k)}
		}
	}
	out := make([]Project, 0, len(byDir))
	for _, p := range byDir {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
```

Note: `ProjectKey` for an agent with only `Repo` returns a leaf, not a dir — acceptable (repo-only agents adopt a leaf-named project). Agents with neither dir nor repo yield "" and are skipped.

- [ ] **Step 4: Run to verify pass**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/core/ -run TestReconcileProjectsAutoAdopts`
Expected: PASS.

- [ ] **Step 5: Full core gate + commit**

```bash
GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/core/
git add internal/core/reconcile_projects.go internal/core/reconcile_projects_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(core): ReconcileProjects auto-adopts dirs with agents"
```

---

## Slice B — host suggestion rename + agent clone/inject plumbing

### Task B1: host/projects.go returns Suggestion

**Files:**
- Modify: `internal/host/projects.go`
- Test: `internal/host/projects_test.go` (if present)

- [ ] **Step 1: Mechanical rename in projects.go**

In `internal/host/projects.go`, replace every `core.Project` with `core.Suggestion` and the internal helper return types likewise. Function names stay (`LocalProjects`, `RemoteProjects`, `GithubProjects`, `ProjectsFromAgents`, `parseGitDirs`) but now return `[]core.Suggestion`. The `Origin`/`Label`/`Repo` field uses are unchanged (Suggestion has the same fields).

- [ ] **Step 2: Fix any test file references**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go build ./internal/host/ 2>&1` — replace `core.Project` with `core.Suggestion` in `internal/host/projects_test.go` if it references the type.

- [ ] **Step 3: Build + test + commit**

```bash
GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/host/
git add internal/host/projects.go internal/host/projects_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "refactor(host): suggestion sources return core.Suggestion"
```

### Task B2: agent Clone accepts explicit dest dir

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Add a CloneTo method (dest-explicit) and keep Clone**

In `internal/agent/agent.go`, refactor `Clone` to delegate to a new `CloneTo`. Replace the body of `Clone` and add `CloneTo`:

```go
// Clone ensures a clone of repo exists under the default repos root (back-compat).
func (a *Agent) Clone(out io.Writer, repo string) int {
	return a.CloneTo(out, repo, filepath.Join(a.reposRoot(), RepoName(repo)))
}

// CloneTo ensures a clone of repo exists at dest and prints {"dir":"<dest>"}.
// If dest already has a .git dir it is reused (no network). An empty repo or
// dest is an error. Used by the engine to mirror a project into its remote path.
func (a *Agent) CloneTo(out io.Writer, repo, dest string) int {
	if strings.TrimSpace(repo) == "" {
		json.NewEncoder(out).Encode(map[string]string{"error": "empty repo"})
		return 1
	}
	if strings.TrimSpace(dest) == "" {
		json.NewEncoder(out).Encode(map[string]string{"error": "empty dest"})
		return 1
	}
	if fi, err := os.Stat(filepath.Join(dest, ".git")); err == nil && fi.IsDir() {
		json.NewEncoder(out).Encode(map[string]string{"dir": dest})
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		json.NewEncoder(out).Encode(map[string]string{"error": err.Error()})
		return 1
	}
	_, code, err := a.Runner.Run("git", []string{"clone", repo, dest}, nil)
	if err != nil || code != 0 {
		json.NewEncoder(out).Encode(map[string]string{"error": "git clone failed"})
		return 1
	}
	json.NewEncoder(out).Encode(map[string]string{"dir": dest})
	return 0
}
```

- [ ] **Step 2: Write failing test for CloneTo reuse**

Add to `internal/agent/agent_test.go`:

```go
func TestCloneToReusesExistingGitDir(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	a := &Agent{Runner: run.Exec{}} // runner not called: .git present
	if code := a.CloneTo(&buf, "git@x:me/proj.git", dest); code != 0 {
		t.Fatalf("code = %d, want 0; out=%s", code, buf.String())
	}
	if !strings.Contains(buf.String(), dest) {
		t.Errorf("output should echo dest dir, got %s", buf.String())
	}
}
```

(Imports needed in the test file: `bytes`, `os`, `path/filepath`, `strings`, and the package's existing `run` import.)

- [ ] **Step 3: Run test**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/agent/ -run TestCloneToReusesExistingGitDir`
Expected: PASS (reuse path returns 0 without invoking git).

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(agent): CloneTo(dest) so a repo can be cloned to a mirrored project path"
```

### Task B3: rbg-agent gains `clone --dir` and `inject --id --text`

**Files:**
- Modify: `cmd/rbg-agent/main.go`

- [ ] **Step 1: Parse the new flags**

In `parseArgs`, extend the `clone` case and add an `inject` case:

```go
	case "clone":
		inv.Repo = flagValue(rest, "--repo")
		inv.Dir = flagValue(rest, "--dir") // optional explicit dest
		if inv.Repo == "" {
			return nil, errors.New("clone requires --repo")
		}
	case "inject":
		inv.Name = flagValue(rest, "--id")
		inv.Task = flagValue(rest, "--text")
		if inv.Name == "" || inv.Task == "" {
			return nil, errors.New("inject requires --id and --text")
		}
```

- [ ] **Step 2: Dispatch the verbs in main()**

Replace the `clone` dispatch case and add `inject`:

```go
	case "clone":
		if inv.Dir != "" {
			os.Exit(a.CloneTo(os.Stdout, inv.Repo, inv.Dir))
		}
		os.Exit(a.Clone(os.Stdout, inv.Repo))
	case "inject":
		home, _ := os.UserHomeDir()
		if err := ptybridge.Inject(home, inv.Name, inv.Task); err != nil {
			fmt.Fprintf(os.Stderr, "rbg-agent: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
```

Add `"github.com/divkov575/rbg/internal/ptybridge"` to imports. (`ptybridge.Inject` is created in Slice E; this slice's build gate for cmd/rbg-agent runs AFTER Slice E — see ordering note. For now, if building B3 alone fails on the missing symbol, that is expected; the slice ordering places E before the final cmd build. To keep B3 independently green, implement B3's `inject` dispatch in the SAME commit as Slice E, OR stub is not allowed — so DEFER the `inject` dispatch wiring to Task E3 and land only `clone --dir` here.)

- [ ] **Step 2b: Land only `clone --dir` in this task**

For B3, add ONLY the `clone` changes (parse `--dir`, dispatch `CloneTo`). Do NOT add the `inject` case yet — it is added in Task E3 alongside `ptybridge.Inject`.

- [ ] **Step 3: Build + commit**

```bash
GOPROXY=off GOFLAGS=-mod=vendor go build ./cmd/rbg-agent/
git add cmd/rbg-agent/main.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(rbg-agent): clone --dir targets an explicit dest"
```

---

## Slice C — engine projects + mirror + spawn wiring

### Task C1: engine holds a ProjectStore; split suggestions from projects

**Files:**
- Modify: `internal/engine/engine.go`
- Modify: `internal/engine/ops.go`

- [ ] **Step 1: Add fields + load store in New**

In `internal/engine/engine.go` `type Engine struct`, add:

```go
	projectStore *core.ProjectStore
	suggestions  func() []core.Suggestion // pickable targets when ADDING a project
	remoteMirror func(dir, repo string) (string, error) // ensure desktop dir+clone; returns remote dir
```

Rename the existing `projects func() []core.Project` field to remove it (projects are now real). In `New`, after loading the agent store:

```go
	projStore, err := core.LoadProjectStore(filepath.Join(filepath.Dir(storePath), "projects.json"))
	if err != nil {
		return nil, err
	}
```

and set `projectStore: projStore` in the struct literal. Replace the `e.projects = func()...` block with:

```go
	e.suggestions = func() []core.Suggestion {
		agents, _ := e.List()
		return core.MergeSuggestions(
			host.LocalProjects(r, localBase),
			host.RemoteProjects(cfg, r, remoteBase),
			host.GithubProjects(r),
			host.ProjectsFromAgents(agents),
		)
	}
	e.remoteMirror = func(dir, repo string) (string, error) {
		return mirrorToRemote(cfg, r, remoteBase, dir, repo)
	}
```

(`mirrorToRemote` is defined in Task C3.)

- [ ] **Step 2: Rewrite Projects(); add Suggestions() in ops.go**

Replace `func (e *Engine) Projects()` in `internal/engine/ops.go`:

```go
// Projects returns the full first-class project list: stored projects plus
// auto-adopted dirs that have agents, with each project's Repo filled by origin
// detection (cached back to the store). Safe on a struct-literal test engine
// (nil store → nil).
func (e *Engine) Projects() []core.Project {
	if e.projectStore == nil {
		return nil
	}
	agents, _ := e.List()
	projs := core.ReconcileProjects(e.projectStore.Records(), agents)
	changed := false
	for i, p := range projs {
		if p.Repo == "" {
			if url := detectOrigin(p.Dir); url != "" {
				projs[i].Repo = url
				e.projectStore.Add(projs[i])
				changed = true
			}
		}
	}
	if changed {
		_ = e.projectStore.Save()
	}
	return projs
}

// Suggestions returns pickable targets for ADDING a project (local/remote/github
// checkouts + in-use repos). nil if no source is wired.
func (e *Engine) Suggestions() []core.Suggestion {
	if e.suggestions == nil {
		return nil
	}
	return e.suggestions()
}
```

- [ ] **Step 3: Build (engine will fail until C2/C3 add detectOrigin/mirrorToRemote)**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go build ./internal/engine/ 2>&1 | head`
Expected: FAIL naming `detectOrigin`/`mirrorToRemote` undefined — resolved in C2/C3. Do not commit until C3.

### Task C2: project CRUD + origin detection

**Files:**
- Create: `internal/engine/projects.go`
- Test: `internal/engine/projects_test.go`

- [ ] **Step 1: Write failing test for AddProject + Rename**

Create `internal/engine/projects_test.go`:

```go
package engine

import (
	"path/filepath"
	"testing"

	"github.com/divkov575/rbg/internal/core"
)

// fakeRunner returns canned output per command for origin detection.
type fakeRunner struct{ url string }

func (f fakeRunner) Run(name string, args []string, stdin []byte) ([]byte, int, error) {
	return []byte(f.url + "\n"), 0, nil
}

func TestAddProjectDetectsOriginAndRenames(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(t.TempDir(), "agents.json")
	ps, _ := core.LoadProjectStore(filepath.Join(t.TempDir(), "projects.json"))
	e := &Engine{
		projectStore: ps,
		detectGit:    func(d string) string { return "git@x:me/app.git" },
	}
	_ = storePath
	p, err := e.AddProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Repo != "git@x:me/app.git" {
		t.Errorf("repo = %q, want detected origin", p.Repo)
	}
	if p.Name != filepath.Base(dir) {
		t.Errorf("name = %q, want dir leaf", p.Name)
	}
	if err := e.RenameProject(p.Dir, "custom"); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.projectStore.Get(p.Dir); got.Name != "custom" {
		t.Errorf("rename not persisted, got %q", got.Name)
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/engine/ -run TestAddProjectDetectsOriginAndRenames`
Expected: FAIL (AddProject/detectGit undefined).

- [ ] **Step 3: Implement projects.go**

Create `internal/engine/projects.go`:

```go
package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/divkov575/rbg/internal/config"
	"github.com/divkov575/rbg/internal/core"
	"github.com/divkov575/rbg/internal/run"
	"github.com/divkov575/rbg/internal/sshx"
)

// AddProject registers an absolute directory as a first-class project, detecting
// its git origin URL. A missing origin leaves Repo empty (a repo-less project).
func (e *Engine) AddProject(dir string) (core.Project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return core.Project{}, fmt.Errorf("add project: %w", err)
	}
	p := core.Project{Dir: abs, Name: filepath.Base(abs), Repo: e.detectOrigin(abs)}
	e.projectStore.Add(p)
	if err := e.projectStore.Save(); err != nil {
		return core.Project{}, fmt.Errorf("add project: save: %w", err)
	}
	return p, nil
}

// RenameProject sets a project's display name.
func (e *Engine) RenameProject(dir, name string) error {
	if !e.projectStore.Rename(dir, name) {
		return fmt.Errorf("rename: no project at %q", dir)
	}
	if err := e.projectStore.Save(); err != nil {
		return fmt.Errorf("rename: save: %w", err)
	}
	return nil
}

// detectOrigin returns the git origin URL for dir, or "" (injectable via
// e.detectGit for tests).
func (e *Engine) detectOrigin(dir string) string {
	if e.detectGit != nil {
		return e.detectGit(dir)
	}
	return detectOrigin(dir)
}

// detectOrigin runs `git -C <dir> remote get-url origin`, trimmed; "" on failure.
func detectOrigin(dir string) string {
	out, code, err := run.Exec{}.Run("git", []string{"-C", dir, "remote", "get-url", "origin"}, nil)
	if err != nil || code != 0 {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// mirrorToRemote ensures the project's mirrored dir exists on the desktop and,
// when repo is set, is cloned there. Returns the remote dir. Idempotent.
func mirrorToRemote(cfg *config.Config, r run.Runner, remoteBase, localDir, repo string) (string, error) {
	if remoteBase == "" {
		return "", fmt.Errorf("mirror: remote base unset (set RBG_CWD)")
	}
	remoteDir := filepath.Join(remoteBase, filepath.Base(localDir))
	if repo == "" {
		// just ensure the dir exists
		args := sshx.BuildSSHArgs(cfg, []string{"mkdir", "-p", remoteDir}, sshx.Options{})
		if _, code, err := r.Run("ssh", args, nil); err != nil || code != 0 {
			return "", fmt.Errorf("mirror: mkdir failed (code %d)", code)
		}
		return remoteDir, nil
	}
	cloneArgs := sshx.AgentArgs(cfg, "clone", []string{"--repo", repo, "--dir", remoteDir})
	args := sshx.BuildSSHArgs(cfg, cloneArgs, sshx.Options{})
	if _, code, err := r.Run("ssh", args, nil); err != nil || code != 0 {
		return "", fmt.Errorf("mirror: remote clone failed (code %d)", code)
	}
	return remoteDir, nil
}
```

- [ ] **Step 4: Add the detectGit injectable field to Engine**

In `internal/engine/engine.go` `type Engine struct`, add:

```go
	detectGit func(dir string) string // origin detection override (tests); nil = real git
```

- [ ] **Step 5: Run test to verify pass**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/engine/ -run TestAddProjectDetectsOriginAndRenames`
Expected: PASS.

### Task C3: MirrorProject + spawn stamps ProjectDir and mirrors

**Files:**
- Modify: `internal/engine/projects.go` (add `MirrorProject`)
- Modify: `internal/engine/ops.go` (`Create` stamps ProjectDir; remote mirrors)
- Test: `internal/engine/projects_test.go`

- [ ] **Step 1: Add MirrorProject to projects.go**

```go
// MirrorProject ensures the project's directory (and clone, if it has a repo)
// exists on the desktop, stamping and persisting the remote path. Idempotent.
func (e *Engine) MirrorProject(dir string) (core.Project, error) {
	p, ok := e.projectStore.Get(dir)
	if !ok {
		return core.Project{}, fmt.Errorf("mirror: no project at %q", dir)
	}
	if e.remoteMirror == nil {
		return core.Project{}, fmt.Errorf("mirror: not wired")
	}
	remote, err := e.remoteMirror(p.Dir, p.Repo)
	if err != nil {
		return core.Project{}, err
	}
	p.Remote = remote
	e.projectStore.Add(p)
	if err := e.projectStore.Save(); err != nil {
		return core.Project{}, fmt.Errorf("mirror: save: %w", err)
	}
	return p, nil
}
```

- [ ] **Step 2: Write failing test — MirrorProject idempotent + stamps Remote**

Add to `internal/engine/projects_test.go`:

```go
func TestMirrorProjectStampsRemote(t *testing.T) {
	ps, _ := core.LoadProjectStore(filepath.Join(t.TempDir(), "projects.json"))
	ps.Add(core.Project{Dir: "/w/app", Name: "app", Repo: "git@x:me/app.git"})
	calls := 0
	e := &Engine{
		projectStore: ps,
		remoteMirror: func(dir, repo string) (string, error) {
			calls++
			return "/desk/workplace/app", nil
		},
	}
	p, err := e.MirrorProject("/w/app")
	if err != nil {
		t.Fatal(err)
	}
	if p.Remote != "/desk/workplace/app" {
		t.Errorf("remote = %q", p.Remote)
	}
	if got, _ := e.projectStore.Get("/w/app"); got.Remote != "/desk/workplace/app" {
		t.Error("remote not persisted")
	}
	if calls != 1 {
		t.Errorf("mirror called %d times, want 1", calls)
	}
}
```

- [ ] **Step 3: Run to verify fail then implement makes it pass**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/engine/ -run TestMirrorProjectStampsRemote`
Expected: FAIL first (MirrorProject undefined), PASS after Step 1.

- [ ] **Step 4: Create stamps ProjectDir; remote spawn mirrors**

In `internal/engine/ops.go` `Create`, after the location switch and before deriving Dir, add project stamping. Replace the `if spec.Repo != "" && spec.Dir == ""` block with:

```go
	// Link to a project and, for a repo-backed project, derive/mirror the dir.
	if spec.ProjectDir != "" {
		if p, ok := e.projectStore.Get(spec.ProjectDir); ok {
			if spec.Repo == "" {
				spec.Repo = p.Repo
			}
			if spec.Where == core.Remote {
				// ensure the desktop has the dir+clone, then run there.
				if mp, err := e.MirrorProject(p.Dir); err == nil && mp.Remote != "" {
					spec.Dir = mp.Remote
				}
			} else if spec.Dir == "" {
				spec.Dir = p.Dir
			}
		}
	}
	if spec.Repo != "" && spec.Dir == "" {
		m := e.pick(spec.Where)
		spec.Dir = core.RepoDir(m.base, m.home, spec.Repo)
	}
```

- [ ] **Step 5: Guard against nil projectStore in Create**

Since struct-literal test engines may have `projectStore == nil`, wrap the new block: `if spec.ProjectDir != "" && e.projectStore != nil {`.

- [ ] **Step 6: Engine build + full engine tests + commit (C1+C2+C3 together)**

```bash
GOPROXY=off GOFLAGS=-mod=vendor go build ./internal/engine/
GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/engine/
git add internal/engine/engine.go internal/engine/ops.go internal/engine/projects.go internal/engine/projects_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(engine): first-class projects (add/rename/mirror), split Suggestions from Projects, spawn stamps ProjectDir"
```

---

## Slice D — render rewrite

### Task D1: Render(jsonl, opts) turn-oriented, rich, with Tail

**Files:**
- Modify: `internal/render/render.go`
- Modify: `internal/render/render_test.go`

- [ ] **Step 1: Write failing tests (golden)**

Replace `internal/render/render_test.go` with:

```go
package render

import (
	"strings"
	"testing"
)

const fixture = `{"type":"user","message":{"role":"user","content":"add a test"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"sure"},{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok\nPASS\nmore\nlines\nhere"}]}}
garbage-not-json
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`

func TestRenderAllTurns(t *testing.T) {
	lines := Render([]byte(fixture), Options{TruncateResult: 2})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "add a test") {
		t.Error("missing user prompt")
	}
	if !strings.Contains(joined, "Bash") || !strings.Contains(joined, "go test") {
		t.Error("tool_use should show name + input summary")
	}
	if !strings.Contains(joined, "+3 more") {
		t.Errorf("tool_result should truncate to 2 lines with marker, got:\n%s", joined)
	}
	if strings.Contains(joined, "garbage-not-json") {
		t.Error("malformed line must be skipped")
	}
	if !strings.Contains(joined, "done") {
		t.Error("final assistant turn missing")
	}
}

func TestRenderTailLimitsTurns(t *testing.T) {
	lines := Render([]byte(fixture), Options{Tail: 1})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "done") {
		t.Error("tail should include the last turn")
	}
	if strings.Contains(joined, "add a test") {
		t.Error("tail=1 should exclude earlier turns")
	}
}

func TestRenderEmpty(t *testing.T) {
	lines := Render(nil, Options{})
	if len(lines) != 1 || !strings.Contains(lines[0], "no conversation") {
		t.Errorf("empty transcript should yield a placeholder, got %v", lines)
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/render/`
Expected: FAIL (Render/Options undefined).

- [ ] **Step 3: Rewrite render.go**

Replace `internal/render/render.go` entirely:

```go
// Package render turns claude transcript JSONL into a rich, human-readable
// reconstruction of the conversation: user/assistant turns with full text, tool
// calls (name + input summary), truncated tool results, and thinking. It
// tolerates unknown keys and malformed lines (skipped).
package render

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Options tunes a render. Tail>0 renders only the last N turns (0 = all).
// TruncateResult caps tool-result lines shown (0 = a sensible default of 8).
type Options struct {
	Tail           int
	TruncateResult int
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}
type record struct {
	Type    string  `json:"type"`
	Message message `json:"message"`
}
type block struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
}

// turn is one rendered role turn (its lines, sans separators).
type turn struct{ lines []string }

// Render reconstructs the conversation from raw JSONL bytes.
func Render(data []byte, opts Options) []string {
	if opts.TruncateResult <= 0 {
		opts.TruncateResult = 8
	}
	var turns []turn
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if t, ok := renderRecord(raw, opts); ok {
			turns = append(turns, t)
		}
	}
	if len(turns) == 0 {
		return []string{"(no conversation content yet)"}
	}
	if opts.Tail > 0 && opts.Tail < len(turns) {
		turns = turns[len(turns)-opts.Tail:]
	}
	var out []string
	for i, t := range turns {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, t.lines...)
	}
	return out
}

func renderRecord(raw string, opts Options) (turn, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return turn{}, false
	}
	var rec record
	if json.Unmarshal([]byte(raw), &rec) != nil {
		return turn{}, false
	}
	role := rec.Message.Role
	if role == "" {
		role = rec.Type
	}
	var body []string
	// content is a bare string or an array of blocks.
	var str string
	if json.Unmarshal(rec.Message.Content, &str) == nil {
		if str != "" {
			body = append(body, str)
		}
	} else {
		var blocks []block
		if json.Unmarshal(rec.Message.Content, &blocks) == nil {
			for _, b := range blocks {
				body = append(body, renderBlock(b, opts)...)
			}
		}
	}
	if len(body) == 0 {
		return turn{}, false
	}
	header := roleHeader(role)
	return turn{lines: append([]string{header}, indent(body)...)}, true
}

func renderBlock(b block, opts Options) []string {
	switch b.Type {
	case "text":
		if b.Text == "" {
			return nil
		}
		return strings.Split(b.Text, "\n")
	case "thinking":
		if b.Text == "" {
			return nil
		}
		return []string{"(thinking) " + firstLine(b.Text)}
	case "tool_use":
		name := b.Name
		if name == "" {
			name = "?"
		}
		return []string{fmt.Sprintf("⚙ %s(%s)", name, inputSummary(b.Input))}
	case "tool_result":
		return truncateResult(b.Content, opts.TruncateResult)
	}
	return nil
}

// inputSummary renders a tool's input JSON to a short one-liner.
func inputSummary(in json.RawMessage) string {
	if len(in) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(in, &m) != nil {
		return ""
	}
	// prefer common single-value keys.
	for _, k := range []string{"command", "file_path", "path", "pattern", "query", "url"} {
		if v, ok := m[k]; ok {
			return firstLine(fmt.Sprintf("%v", v))
		}
	}
	// else list keys.
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ",")
}

// truncateResult renders a tool_result's content (string or blocks) to at most
// n lines, appending a "+M more" marker when clipped.
func truncateResult(content json.RawMessage, n int) []string {
	text := resultText(content)
	if text == "" {
		return []string{"[tool result]"}
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return append([]string{"[tool result]"}, lines...)
	}
	shown := append([]string{"[tool result]"}, lines[:n]...)
	return append(shown, fmt.Sprintf("… +%d more", len(lines)-n))
}

func resultText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(content, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func roleHeader(role string) string {
	switch role {
	case "user":
		return "▸ you"
	case "assistant":
		return "▸ claude"
	case "":
		return "▸ ?"
	default:
		return "▸ " + role
	}
}

func indent(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "  " + l
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/render/`
Expected: PASS.

- [ ] **Step 5: Fix render.Line callers**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go build ./... 2>&1 | grep render` — the only callers are in `internal/uitea` (fixed in Slice G). If `internal/cli/render.go` or others call `render.Line`/`render.Stream`, note them for the caller's slice. Do not fix uitea here.

- [ ] **Step 6: Commit**

```bash
git add internal/render/render.go internal/render/render_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(render): rich turn-oriented Render(jsonl, opts) with Tail + truncation; drop Line/Stream"
```

---

## Slice E — ptybridge trim + inject

### Task E1: Delete terminal-attach files

**Files:**
- Delete: `internal/ptybridge/bridge.go`, `internal/ptybridge/attach.go`, `internal/ptybridge/bridge_test.go`

- [ ] **Step 1: Remove the files**

```bash
git rm internal/ptybridge/bridge.go internal/ptybridge/attach.go internal/ptybridge/bridge_test.go
```

- [ ] **Step 2: Verify frame.go + roster.go still build/test**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/ptybridge/`
Expected: PASS (frame_test.go + roster_test.go remain).

- [ ] **Step 3: Commit**

```bash
git -c user.email="dmitriy@ivkov.net" commit -m "refactor(ptybridge): drop terminal-attach bridge; keep codec + roster"
```

### Task E2: Inject(session, text)

**Files:**
- Create: `internal/ptybridge/inject.go`
- Create: `internal/ptybridge/inject_test.go`

- [ ] **Step 1: Write failing test against a net.Pipe fake server**

Create `internal/ptybridge/inject_test.go`:

```go
package ptybridge

import (
	"net"
	"testing"
	"time"
)

func TestInjectSendsHelloThenStdin(t *testing.T) {
	client, server := net.Pipe()
	got := make(chan string, 1)
	go func() {
		defer server.Close()
		// server speaks first: hello
		_ = WriteCtrl(server, Ctrl{T: "hello", ReplPid: 1})
		// then expect a KindData stdin frame carrying the text + CR
		kind, payload, err := ReadFrame(server)
		if err != nil || kind != KindData {
			got <- ""
			return
		}
		got <- string(payload)
	}()

	// injectConn is the socket-level core of Inject, taking an open conn so the
	// test can supply net.Pipe instead of dialing a unix socket.
	err := injectConn(client, "hello world")
	if err != nil {
		t.Fatalf("injectConn: %v", err)
	}
	select {
	case s := <-got:
		if s != "hello world\r" {
			t.Errorf("server received %q, want %q", s, "hello world\r")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received the stdin frame")
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/ptybridge/ -run TestInjectSendsHelloThenStdin`
Expected: FAIL (injectConn undefined).

- [ ] **Step 3: Implement inject.go**

Create `internal/ptybridge/inject.go`:

```go
package ptybridge

import (
	"fmt"
	"io"
	"net"
	"time"
)

// Inject delivers text as a single line of stdin to the live bg agent for
// session id (resolved via the daemon roster under home). It connects the pty
// socket, reads the server's hello, writes the text followed by a carriage
// return as a KindData frame, then closes. It does NOT attach the terminal —
// this is fire-and-forget input delivery so a follow-up reaches a running agent
// that `claude --resume` would refuse.
func Inject(home, id, text string) error {
	w, err := FindWorker(home, id)
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", w.PtySock, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect pty socket: %w", err)
	}
	defer conn.Close()
	return injectConn(conn, text)
}

// injectConn performs the hello-read + stdin-write handshake over an open conn.
func injectConn(conn io.ReadWriter, text string) error {
	kind, _, err := ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	if kind != KindCtrl {
		return fmt.Errorf("expected hello control frame, got kind %d", kind)
	}
	// A carriage return submits the line to claude's input.
	return WriteData(conn, []byte(text+"\r"))
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/ptybridge/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ptybridge/inject.go internal/ptybridge/inject_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(ptybridge): Inject(session,text) delivers a follow-up line to a live agent"
```

### Task E3: Wire rbg-agent inject; drop rbg attach

**Files:**
- Modify: `cmd/rbg-agent/main.go`
- Modify: `cmd/rbg/main.go`

- [ ] **Step 1: Add the inject parse case + dispatch (from B3 Step 1/2)**

In `cmd/rbg-agent/main.go` add the `inject` parse case and the `inject` dispatch case exactly as written in Task B3 Step 1 and Step 2, plus the `ptybridge` import. Also remove the `attach` verb (parse case + dispatch) added previously.

- [ ] **Step 2: Remove attach from cmd/rbg**

In `cmd/rbg/main.go`: delete the `attach` entry from the `case "deploy", "ping", "attach":` line (→ `case "deploy", "ping":`), delete the `case "attach":` block in `runLegacy`, and delete the `attach(...)` function and the now-unused `claudeSessionIDFor` if nothing else uses it (grep first). Remove now-unused imports.

- [ ] **Step 3: Build both cmds + cross-compile agent**

```bash
GOPROXY=off GOFLAGS=-mod=vendor go build ./cmd/rbg/ ./cmd/rbg-agent/
GOPROXY=off GOFLAGS=-mod=vendor GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/rbg-agent/
```
Expected: all succeed.

- [ ] **Step 4: Commit**

```bash
git add cmd/rbg-agent/main.go cmd/rbg/main.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(cli): rbg-agent inject verb; remove terminal attach from rbg/rbg-agent"
```

---

## Slice F — engine Send routing (done vs live)

### Task F1: Send routes live→inject, done→resume

**Files:**
- Modify: `internal/engine/control.go`
- Test: `internal/engine/control_test.go`

- [ ] **Step 1: Read current Send**

Run: `sed -n '96,111p' internal/engine/control.go` to see the exact current body (it calls `e.pick(a.Where).newRunner(a.Dir).Send(a.Session, task)`).

- [ ] **Step 2: Write failing test — live agent injects, done resumes**

Add to `internal/engine/control_test.go` (create if absent, following the package's existing test engine construction):

```go
func TestSendLiveInjectsDoneResumes(t *testing.T) {
	var injected, resumed string
	e := &Engine{
		injectLive: func(where core.Location, session, text string) error {
			injected = session + "|" + text
			return nil
		},
		resumeSend: func(where core.Location, dir, session, text string) error {
			resumed = session + "|" + text
			return nil
		},
	}
	live := core.Agent{Name: "L", Session: "S-live", State: core.Running, Where: core.Local}
	done := core.Agent{Name: "D", Session: "S-done", State: core.Done, Where: core.Local}
	if err := e.sendTo(live, "hi-live"); err != nil {
		t.Fatal(err)
	}
	if injected != "S-live|hi-live" {
		t.Errorf("live send should inject, got %q", injected)
	}
	if err := e.sendTo(done, "hi-done"); err != nil {
		t.Fatal(err)
	}
	if resumed != "S-done|hi-done" {
		t.Errorf("done send should resume, got %q", resumed)
	}
}
```

- [ ] **Step 3: Run to verify fail**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/engine/ -run TestSendLiveInjectsDoneResumes`
Expected: FAIL (sendTo/injectLive/resumeSend undefined).

- [ ] **Step 4: Add fields + refactor Send to route**

In `internal/engine/engine.go` `type Engine struct`, add:

```go
	injectLive func(where core.Location, session, text string) error
	resumeSend func(where core.Location, dir, session, text string) error
```

In `New`, wire the real closures (local injects in-process via ptybridge; remote via rbg-agent inject; resume uses the existing runner path):

```go
	e.injectLive = func(where core.Location, session, text string) error {
		if where == core.Local {
			return ptybridge.Inject(home, session, text)
		}
		args := sshx.BuildSSHArgs(cfg, sshx.AgentArgs(cfg, "inject", []string{"--id", session, "--text", text}), sshx.Options{})
		if _, code, err := r.Run("ssh", args, nil); err != nil || code != 0 {
			return fmt.Errorf("remote inject failed (code %d)", code)
		}
		return nil
	}
	e.resumeSend = func(where core.Location, dir, session, text string) error {
		return e.pick(where).newRunner(dir).Send(session, text)
	}
```

Add imports `ptybridge` and `fmt`/`sshx` as needed (sshx already imported). In `control.go`, replace the body of `Send` to resolve then delegate to `sendTo`, and add `sendTo`:

```go
func (e *Engine) Send(name, task string) error {
	a, err := e.find(name)
	if err != nil {
		return err
	}
	return e.sendTo(a, task)
}

// sendTo delivers a follow-up: a LIVE agent (Running) is refused by
// `claude --resume` because a process owns its session, so we inject the text as
// stdin over the daemon socket; a DONE agent is resumed (appends to the same
// transcript).
func (e *Engine) sendTo(a core.Agent, task string) error {
	if a.Session == "" {
		return fmt.Errorf("agent %q has not run yet (no session)", a.Name)
	}
	if a.State == core.Running && e.injectLive != nil {
		return e.injectLive(a.Where, a.Session, task)
	}
	if e.resumeSend != nil {
		return e.resumeSend(a.Where, a.Dir, a.Session, task)
	}
	return e.pick(a.Where).newRunner(a.Dir).Send(a.Session, task)
}
```

(Keep any existing pre-checks from the old `Send` — e.g. done-agent handling — folded into `sendTo`.)

- [ ] **Step 5: Run to verify pass + full engine tests**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go test ./internal/engine/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/engine.go internal/engine/control.go internal/engine/control_test.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(engine): Send routes live agents to socket inject, done agents to resume"
```

---

## Slice G — uitea rewire

### Task G1: Ops interface + pager fed by render.Render

**Files:**
- Modify: `internal/uitea/uitea.go`
- Modify: `internal/uitea/pager.go`

- [ ] **Step 1: Extend Ops with project methods**

In `internal/uitea/uitea.go`, change the `Ops` interface: keep existing methods, change `Projects()` return, add project ops:

```go
type Ops interface {
	List() ([]core.Agent, error)
	Create(spec core.Agent) (core.Agent, error)
	Run(name string) error
	Send(name, task string) error
	Read(name string) ([]byte, error)
	Kill(name string) error
	Adopt(name string) error
	Projects() []core.Project
	Suggestions() []core.Suggestion
	AddProject(dir string) (core.Project, error)
	RenameProject(dir, name string) error
	RepairRemote() (bool, error)
}
```

- [ ] **Step 2: Feed the pager from render.Render**

In `internal/uitea/pager.go`, replace `renderTranscript` and `setTranscript`:

```go
// setTranscript re-renders lines from raw JSONL and clears loading. tailOnly
// renders just the latest turn (instant first paint); full renders everything.
func (p pagerModel) setTranscript(data []byte, tailOnly bool) pagerModel {
	opts := render.Options{}
	if tailOnly {
		opts.Tail = 1
	}
	p.lines = render.Render(data, opts)
	p.loading = false
	p.offset = -1 // pin to bottom
	return p
}
```

Delete the old `renderTranscript` function and its `strings`/`render.Line` usage. Keep the `render` import.

- [ ] **Step 3: Build (uitea will fail until update.go/view.go updated)**

Run: `GOPROXY=off GOFLAGS=-mod=vendor go build ./internal/uitea/ 2>&1 | head`
Expected: FAIL referencing changed signatures — resolved in G2/G3. Do not commit yet.

### Task G2: Open path, tail tick, transcript fill

**Files:**
- Modify: `internal/uitea/uitea.go` (commands)
- Modify: `internal/uitea/update.go`

- [ ] **Step 1: Add a tail-tick command + a full-read follow-up**

In `internal/uitea/uitea.go`, add:

```go
// tailCmd schedules the next transcript refresh for a live session view.
func tailCmd() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return tailTick{} })
}
```

Add message type in the messages section: `type tailTick struct{}`. Add a `full bool` to `transcriptMsg`:

```go
type transcriptMsg struct {
	name string
	data []byte
	full bool // false = first paint (tail only), true = full scrollback
	err  error
}
```

Update `readCmd` to carry `full`:

```go
func (m Model) readCmd(name string, full bool) tea.Cmd {
	ops := m.ops
	return func() tea.Msg {
		data, err := ops.Read(name)
		return transcriptMsg{name: name, data: data, full: full, err: err}
	}
}
```

- [ ] **Step 2: Update transcriptMsg handling + add tailTick handling in update.go**

In `internal/uitea/update.go`, replace the `transcriptMsg` case:

```go
	case transcriptMsg:
		if m.mode != modePager || m.pager.agent != msg.name {
			return m, nil
		}
		if msg.err != nil {
			m.pager.loading = false
			m.pager.status = "read failed: " + friendlyErr(msg.err)
			return m, nil
		}
		m.pager = m.pager.setTranscript(msg.data, !msg.full)
		if !msg.full {
			// first paint done (tail); fetch the full scrollback next.
			return m, m.readCmd(msg.name, true)
		}
		return m, nil

	case tailTick:
		// keep tailing while the opened agent is live.
		if m.mode != modePager {
			return m, nil
		}
		if !m.pagerAgentLive() {
			return m, nil
		}
		return m, tea.Batch(m.readCmd(m.pager.agent, true), tailCmd())
```

- [ ] **Step 3: Add pagerAgentLive helper + kick tail on open**

In `internal/uitea/update.go` `openSelected`, replace the pager-open block:

```go
	m.pager = newSessionView("session · "+a.Name, a.Session)
	m.mode = modePager
	cmds := []tea.Cmd{m.readCmd(a.Session, false), spinCmd()}
	if a.State == core.Running {
		cmds = append(cmds, tailCmd())
	}
	return m, tea.Batch(cmds...)
```

Delete the `if a.Where == core.Local && a.Session != ""` branch and the `openClientCmd` call (one render path for all). Add helper at end of update.go:

```go
// pagerAgentLive reports whether the agent shown in the pager is still Running.
func (m Model) pagerAgentLive() bool {
	for _, a := range m.agents {
		if a.Session == m.pager.agent {
			return a.State == core.Running
		}
	}
	return false
}
```

- [ ] **Step 4: Update the other readCmd caller (sentMsg re-read)**

In the `sentMsg` case in update.go, change `m.readCmd(msg.name)` → `m.readCmd(msg.name, true)`.

- [ ] **Step 5: Delete openClientCmd**

Remove `func (m Model) openClientCmd` from `internal/uitea/uitea.go` and the now-unused `os/exec` import.

### Task G3: Project view primary + add/rename keys + picker

**Files:**
- Modify: `internal/uitea/view.go`
- Modify: `internal/uitea/update.go`
- Modify: `internal/uitea/picker.go`
- Modify: `internal/uitea/lens.go`

- [ ] **Step 1: Default to project view**

In `internal/uitea/uitea.go` `New`, change `view: viewRemote` → `view: viewProject`.

- [ ] **Step 2: projectRows uses real projects + badges**

In `internal/uitea/view.go`, update `projectRows` to pass projects and render badges:

```go
func (m Model) projectRows(nameW int) string {
	projects := m.ops.Projects()
	groups := core.GroupByProject(m.agents, projects)
	if len(groups) == 0 {
		return stHints.Render("  (no projects — ^n to add)") + "\n"
	}
	byDir := map[string]core.Project{}
	for _, p := range projects {
		byDir[p.Dir] = p
	}
	var b strings.Builder
	base := 0
	for _, g := range groups {
		label := projectHeader(g.Project, byDir[g.Project])
		b.WriteString(stSection.Render(label) + "\n")
		b.WriteString(m.rows(g.Agents, base, nameW))
		base += len(g.Agents)
	}
	return b.String()
}

// projectHeader renders a project section title with repo + mirror badges.
func projectHeader(key string, p core.Project) string {
	name := p.Name
	if name == "" {
		if key == "" {
			return "(unlinked)"
		}
		name = core.ProjectKeyLeaf(key)
	}
	repo := "no repo"
	if p.Repo != "" {
		repo = core.ProjectKeyLeaf(p.Repo)
	}
	badge := ""
	if p.Remote != "" {
		badge = "  ⇄ mirrored"
	}
	return fmt.Sprintf("%s  [%s]%s", name, repo, badge)
}
```

- [ ] **Step 2b: Add ProjectKeyLeaf helper to core**

In `internal/core/lenses.go`, export the leaf helper for the view:

```go
// ProjectKeyLeaf is the display leaf of a project key or repo string.
func ProjectKeyLeaf(s string) string { return leaf(s) }
```

- [ ] **Step 3: Rework picker for add-project (dir + suggestions)**

In `internal/uitea/picker.go`, the picker's `all` becomes `[]core.Suggestion`; `noRepoProject` becomes a `core.Suggestion`. On spawn/choose it yields a chosen dir/repo. Change the type refs `core.Project`→`core.Suggestion`. The two-stage flow stays, but choosing now feeds `AddProject`. Add a `chosenRepo`/`chosenDir` accessor. (Mechanical type swap + rename `newPicker(projects []core.Suggestion)`.)

- [ ] **Step 4: Wire ^n add-project and ^e rename in keyList**

In `internal/uitea/update.go` `keyList`, change the `"new"` case to open the picker with `m.ops.Suggestions()`, and after a pick call `m.ops.AddProject(chosen)`. Add an `"rename"` case:

```go
	case "rename":
		if p, ok := m.selectedProject(); ok {
			m.mode = modeRename
			m.renameBuf = p.Name
			m.renameDir = p.Dir
		}
		return m, nil
```

Add `modeRename` to the mode enum in uitea.go, `renameBuf`/`renameDir` fields to Model, a `selectedProject()` helper (maps cursor→project in project view), and handle `modeRename` keystrokes in `handleKey` (runes edit renameBuf, enter → `m.ops.RenameProject(renameDir, renameBuf)` then back to list, esc cancels). Map `^e` to `"rename"` in `keyName` (`tea.KeyCtrlE`).

- [ ] **Step 5: Update spawnFromPrompt / spawnFromPicker to stamp ProjectDir**

In `spawnFromPrompt` and `spawnFromPicker`, set `spec.ProjectDir` from the selected project's Dir (project view) or the selected agent's ProjectDir/Dir. Keep machine-from-lens logic.

- [ ] **Step 6: Fix the fake Ops in tests**

In `internal/uitea/uitea_test.go`, extend `recOps` with `Suggestions() []core.Suggestion`, `AddProject(dir string) (core.Project, error)`, `RenameProject(dir, name string) error`, and change `Projects()` to return `[]core.Project`. Update any test asserting `transcriptMsg{name:...}` / `readCmd` arity to the new signatures (session-id key retained).

- [ ] **Step 7: Full build + full test**

```bash
GOPROXY=off GOFLAGS=-mod=vendor go build ./...
GOPROXY=off GOFLAGS=-mod=vendor go test ./...
```
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/uitea/ internal/core/lenses.go
git -c user.email="dmitriy@ivkov.net" commit -m "feat(uitea): project view primary; ^n add / ^e rename; one render+tail open path; drop claude --resume open"
```

---

## Slice H — final integration gate

### Task H1: Whole-repo build, test, cross-compile, deploy check

**Files:** none (verification only)

- [ ] **Step 1: Whole build + test**

```bash
GOPROXY=off GOFLAGS=-mod=vendor go build ./...
GOPROXY=off GOFLAGS=-mod=vendor go test ./...
GOPROXY=off GOFLAGS=-mod=vendor go vet ./...
```
Expected: all clean.

- [ ] **Step 2: Cross-compile the agent for the desktop**

```bash
GOPROXY=off GOFLAGS=-mod=vendor GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/rbg-agent/
GOPROXY=off GOFLAGS=-mod=vendor GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null ./cmd/rbg-agent/
```
Expected: both succeed.

- [ ] **Step 3: Deploy the updated agent to the desktop**

```bash
go run ./cmd/rbg deploy   # cross-compiles + scps rbg-agent to the desktop
```
Expected: "deployed rbg-agent (linux/…) to …". (Requires desktop reachable; if not, note it and skip.)

- [ ] **Step 4: Smoke-test live inject against a NON-48fd50b3 worker**

Confirm `ptybridge.Inject` lands a real keystroke on a live scratch worker (not this session) by injecting a harmless newline and observing the transcript grow. Document the result.
