# Workspace design

Status: proposed 2026-09-21, not implemented. Measured against the tree and
the code on that date; see the evidence section. A second reviewer checked
the scenarios in the last section.

## The problem

Dockhand has six names for one idea. `git.Snapshot` is a whole tree written to
a temporary directory. `macports.Tree` is a root with its source and platform,
and `macports.Context` adds a target. `portedit.workspace` is a root with its
source, `portedit.sourceInput` is that plus the target, the baseline, an
interpreter session, and two caches, and `survey.Workspace` is a root with its
ports and index. Every one is a directory that exists for one reason: MacPorts
evaluates files. The evaluator starts `port-tclsh` in the root, points
`sources.conf` at it, and opens `file://<root>/<category>/<port>`. The truth
is always the git tree; the directory is a projection of it.

The projection is expensive and repeated. Materializing the ports tree writes
34,614 files in 3.2 seconds and removes them in 2.5, about six seconds a round
trip. One `bump` does it five or six times: release resolution and preparation
each open the same tree (`preparation.Service.open`), preparation materializes
the candidate tree in full to evaluate one port again, the index cache
materializes when handed no root, dependent discovery materializes, and
staging materializes once more to pack the tree for the VM. Candidates are
evaluated by writing contents over the Portfile in place and restoring
(`workspace.withContents`), so a multi-profile observation writes once and
reads many, and an interrupted preparation can leave the directory dirty.

## The design in one paragraph

A workspace is a lazily materialized, scoped, read-only projection of one git
tree. Materialization happens by scope: a port's directory and `_resources`
for evaluation, the whole tree for indexing, staging, and the survey. An edit
is never written into a workspace; it is an overlay, a sibling projection
that shares the base's files and replaces the edited ones, and whose git tree
is produced by the same edits. The workspace owns the interpreter sessions
bound to its root. One registry hands out one workspace per tree per process.
A sparse projection is proven, not assumed: the evaluator's read interposer
reports any in-tree read outside the scope, and the workspace widens to the
whole tree and evaluates again.

## Package

`internal/macports/workspace`, importing `git`, `record`, `macports`, and
`macports/portfile`. It replaces `git.Snapshot`, `portedit.workspace`, and
the root-plus-source parts of `sourceInput` and `survey.Workspace`.
`macports.Tree` and `macports.Context` stay as the values the evaluator takes;
the workspace produces them.

```go
// Workspace projects one immutable source tree onto a directory MacPorts can
// read. It materializes on demand, by scope, and never rewrites a file it
// wrote; an edit is an Overlay.
type Workspace struct { /* repo, source, directory, present paths, sessions */ }

// Open claims a directory for the tree and materializes nothing.
func Open(ctx context.Context, repo *git.Repository, source record.Source) (*Workspace, error)

func (w *Workspace) Source() record.Source
func (w *Workspace) Root() string

// EnsurePort materializes _resources and the target's port directory,
// including its files/ tree. It is idempotent and cheap when present.
func (w *Workspace) EnsurePort(ctx context.Context, target record.Target) error

// EnsureAll materializes the whole tree; later Ensure calls are no-ops.
func (w *Workspace) EnsureAll(ctx context.Context) error

// Scope reports what is present: the port directories and whether the whole
// tree is; _resources is always present after any Ensure.
func (w *Workspace) Scope() Scope

// Tree and Context are the evaluator's views of the root.
func (w *Workspace) Tree(platform record.Platform) (macports.Tree, error)
func (w *Workspace) Context(target record.Target, platform record.Platform) (macports.Context, error)

// Batch is the interpreter session bound to the root, opened on first use
// and shared until Close, as sourceInput.native does today.
func (w *Workspace) Batch(ctx context.Context, ports macports.BatchReader) (macports.Batch, error)

// Overlay is a sibling projection with the edits applied: unchanged files
// are hardlinked from the base, edited files are written, and the overlay's
// scope is the base's. Its git tree is not computed until Commit.
func (w *Workspace) Overlay(ctx context.Context, edits []git.FileEdit) (*Overlay, error)

// Commit writes the overlay's edits as git objects over the base tree and
// returns the source whose tree they make; the overlay's files are, by
// construction, that tree's projection over the base's scope.
func (o *Overlay) Commit(ctx context.Context) (record.Source, error)

func (w *Workspace) Close() error
```

`Scope` is `{Ports []string; All bool}` with port directories as
`category/port`. `EnsurePort` on a target whose directory is already present
returns at once. `EnsureAll` on a workspace with ports present materializes
the rest. An overlay is a `*Workspace` in every method above, so a candidate
can be observed, evaluated, or overlaid again.

Materialization reuses `Materialize`'s body with pathspecs: `git ls-tree -rz
<tree> -- _resources <category>/<port>` for a port, no pathspec for all, and
`git cat-file --batch` for the blobs, with the same mode and path checks. An
overlay's shared files are hardlinks into the base directory, so they cost a
directory walk and no data; the workspace's own files are never opened for
writing after creation, which is what makes the links safe.

### Registry

```go
// Registry hands out one workspace per tree within a process and closes it
// when the last holder releases it. Overlays are not registered.
type Registry struct { /* repo, workspaces by tree, counts */ }
func (r *Registry) Acquire(ctx context.Context, source record.Source) (*Workspace, func() error, error)
```

`app` owns one registry per command; the preparation, dependent discovery,
index staging, and Tart staging acquire the tree through it. The index
closure in `app/selection.go` already keeps a `staged` map for the same
reason; it moves into the registry.

### Blob store, later and measured

A content-addressed store beside the index cache, keyed by blob id, from
which a projection is hardlink creation. Two adjacent master trees differ by
a few hundred files, so the second full projection costs a walk, not 800 MB.
Not part of the first change; it is the answer if the survey's or staging's
full materialization shows up in a profile after the rest lands.

## The invariant, and how it is proven

Evaluating target T in a workspace whose scope holds T's directory and
`_resources` yields the evaluation the whole tree would, provided the
evaluation reads nothing else in the tree.

The proviso is the design's one assumption, and it is enforced rather than
trusted. The evaluator's read interposer in `eval/observation.tcl` already
resolves every `open`, `source`, `file`, and `glob` path and classifies a
path outside the root as host state. It gains the workspace's scope, and a
path inside the root but outside the scope becomes its own event, "depends
on files outside the port's directory and shared resources", naming the
path. A workspace-bound session installs the interposer in that mode for
every evaluation, not only observations, because a `file exists` on a
missing in-tree path takes a branch silently where an `open` would fail
loudly. When the event fires the workspace widens with `EnsureAll` and the
caller evaluates once more; the widening is reported at debug level with the
path, so a Portfile that needs it is found rather than guessed.

Evidence that the assumption holds today, measured on the tree at
2026-09-21 with 20,107 Portfiles and 145 files under `_resources`:

- No Portfile climbs above its directory: no `${portpath}/..`, no
  `[file dirname ${portpath}]`, and every `../` in a Portfile is a build
  path inside `worksrcpath` or a distfile path.
- No Portfile sources a tree file; every `source` is prose or `${prefix}`.
- No Portfile or PortGroup reads the PortIndex or another port at
  evaluation; `mportlookup` and `mportopen` appear in neither.
- PortGroups read `${filespath}`, which is the port's own `files/`, and
  `_resources`; `host_access.go` already models the group directory reads.
- The evaluator's `initialize` sets `sources` to `file://<root>` and opens
  the port by path; it never reads the index. Name resolution reads the
  staged `PortIndex` files copied into the root, which a sparse workspace
  holds like a full one.
- The baseline survey's 41,730 evaluations recorded 700-odd host reads,
  every one "outside the captured ports tree"; the tracer tolerates in-tree
  reads elsewhere silently today, which is the gap the scope event closes.

## What each consumer does

| consumer | today | with workspaces |
| --- | --- | --- |
| release resolution and preparation (`workflow/preparation`) | two full materializations of the source tree, one more of the candidate | one workspace from the registry, `EnsurePort` for the target; candidates are overlays; the final check evaluates the committed overlay, sparse |
| candidate evaluation in `portedit` | `withContents` writes over the Portfile, evaluates, restores | `Overlay` per candidate; the probe's version loop overlays the Portfile only; observation reads immutable directories |
| assess and outdated (`survey`) | one full materialization, index staged, every port evaluated in it | `EnsureAll` once, unchanged otherwise; offline |
| verify binding (`workflow/verification_bind`) | full materialization to resolve one target | `EnsurePort` after name resolution against the staged index; working-tree capture writes its tree first as today, then the same |
| Tart staging (`verify/staging`) | full materialization, index, tar for the VM | the registry's workspace, `EnsureAll`, same tar |
| GitHub verification | no materialization | unchanged |
| dependent discovery (`macports/dependents`) | full materialization, index, evaluates many ports | the registry's workspace, `EnsureAll`, since dependents are arbitrary ports |
| index cache (`portindex.ensure` with no root) | private full materialization | the registry's workspace, `EnsureAll` |
| adopt, corrections, prepare-onto | read files through `repo.File`, then the preparation path | unchanged, then as preparation |
| patch check and dependency generators | read `files/` under the port directory and extracted archives | `EnsurePort` covers `files/`; archives are outside the tree as today |

A shared release edits one Portfile with its subports, one directory. A
future multi-target contribution ensures one port per target. A stub and its
carrier are one Portfile.

### Contributions that edit `_resources`

A candidate that changes a PortGroup is a normal overlay: `_resources` is in
every scope, so its evaluation sees the edited group. The index treats such
a tree as it does today, a full pass, since `requiresFullIndex` keys on the
same prefix. Verification stages the whole tree. The working-tree capture
already admits untracked files under `_resources/`. What the design does not
add is a contribution whose only change is under `_resources`, with no port
to evaluate; that has no target today either.

## Sequence

1. `portedit/archives` split, unrelated to this and safe now.
2. `workspace` with `Open`, `EnsurePort`, `EnsureAll`, `Overlay`, `Commit`,
   and the scope event in the interposer; adopted by `workflow/preparation`
   and `portedit`, where the five materializations and the in-place mutation
   live. `sourceInput` shrinks to the workspace, the target, the baseline,
   and the release inputs. Measured on a real bump before and after.
3. `portedit/observe` on top of the workspace: it takes a workspace, a
   target, and a platform, and observes immutable directories.
4. The registry, adopted by staging, dependent discovery, and the index
   cache, replacing `git.Snapshot` everywhere; measured on a bump with
   verification.
5. The blob store, only if a profile asks for it.

## Risks

- A Portfile that reads another port's files at evaluation would widen the
  workspace on first evaluation and cost one repeat; none exists in the tree
  today, and the event names it if one appears.
- Hardlinked overlays require the base and the overlay on one filesystem;
  the workspace directory holds both. A tool that writes through a link
  would corrupt the base; nothing in dockhand writes into a workspace, and
  the base's files are read-only after creation.
- The preparation's final equivalence check exists to prove the in-place
  evaluation matched the committed tree. Overlays make the two the same
  bytes by construction; the check stays and evaluates the sparse overlay,
  which is cheaper, not weaker.
- Registry sharing means a workspace is in use by more than one consumer in
  one process; it is read-only, and overlays are private, so sharing is
  safe. Closing waits for the last release.
