# Workspace design

Status: proposed 2026-09-21, not implemented. Measured against the tree and
the code on that date; see the evidence section. A second reviewer, on a
different model, attacked the first draft scenario by scenario; the findings
that held are folded in and listed in the last section.

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
for the preparation path, the whole tree for everything that resolves names,
indexes, stages, or evaluates arbitrary ports. An edit is never written into
a workspace; it is an overlay, a sibling projection that shares the base's
tracked files and replaces the edited ones, and whose git tree is produced
by the same edits. The workspace owns the interpreter sessions bound to its
root, and a session serves the overlays of its root. One registry hands out
one workspace per tree per process. A sparse projection rests on a measured
property of the tree, that a Portfile's evaluation reads only its own
directory and `_resources`, and every consumer that could be wrong about it
is given the whole tree instead.

## Package

`internal/macports/workspace`, importing `git`, `record`, `macports`, and
`macports/portfile`. It replaces `git.Snapshot`, `portedit.workspace`, and
the root-plus-source parts of `sourceInput` and `survey.Workspace`.
`macports.Tree` and `macports.Context` stay as the values the evaluator takes;
the workspace produces them, and a `Tree` learns to name its base root so an
interpreter session can tell an overlay from a stranger.

```go
// Workspace projects one immutable source tree onto a directory MacPorts can
// read. It materializes on demand, by scope, and never rewrites a tracked
// file it wrote; an edit is an Overlay.
type Workspace struct { /* repo, source, directory, base, present paths, sessions */ }

// Open claims a directory for the tree, creates it, and materializes
// nothing into it.
func Open(ctx context.Context, repo *git.Repository, source record.Source) (*Workspace, error)

func (w *Workspace) Source() record.Source
func (w *Workspace) Root() string

// EnsurePort materializes _resources and the target's port directory,
// including its files/ tree. It is idempotent and cheap when present.
func (w *Workspace) EnsurePort(ctx context.Context, target record.Target) error

// EnsureAll materializes the whole tree; later Ensure calls are no-ops.
func (w *Workspace) EnsureAll(ctx context.Context) error

// Scope reports what is present: the port directories and whether the whole
// tree is; _resources is present after any Ensure.
func (w *Workspace) Scope() Scope

// Tree and Context are the evaluator's views of the root. Both carry the
// base root, the workspace's own for a base and the base's for an overlay.
func (w *Workspace) Tree(platform record.Platform) (macports.Tree, error)
func (w *Workspace) Context(target record.Target, platform record.Platform) (macports.Context, error)

// Batch is the interpreter session bound to the base root, opened on first
// use and shared by the base and its overlays until Close, as
// sourceInput.native does today. An overlay's Batch is its base's.
func (w *Workspace) Batch(ctx context.Context, ports macports.BatchReader) (macports.Batch, error)

// Overlay is a sibling projection with the edits applied. Tracked files the
// edits do not touch are hardlinked from the base, symlinks are re-created
// from their tree entries, edited files are written with their entries'
// modes, and the overlay's scope is the base's. Untracked files in the base
// directory, the staged index among them, are not part of an overlay. The
// overlay's git tree is not computed until Commit.
func (w *Workspace) Overlay(ctx context.Context, edits []git.FileEdit) (*Workspace, error)

// Commit writes the overlay's edits as git objects over the base tree and
// returns the source whose tree they make; the overlay's files are, by
// construction, that tree's projection over the base's scope.
func (w *Workspace) Commit(ctx context.Context) (record.Source, error)

// Close closes the sessions, then removes the directory. Overlays close
// before their base.
func (w *Workspace) Close() error
```

`Scope` is `{Ports []string; All bool}` with port directories as
`category/port`. Materialization reuses `Materialize`'s body with pathspecs:
`git ls-tree -rz <tree> -- _resources <category>/<port>` for a port, no
pathspec for all, and `git cat-file --batch` for the blobs, with the same
mode, path, and symlink checks `extractBlobs` makes today. An overlay is
built from the base's tree entries, never from a walk of the base directory,
which is what keeps untracked files and symlinks out of the links; on macOS
a hardlink to a symlink is a hardlink to its target, and the nine symlinks
under `_resources/port1.0/checks` would silently become files.

### Root identity

An overlay is a different directory, and two things in dockhand assume one
root per comparison. `fidelity.ComparablePort` replaces one root string with
`<source>` in every option value, and `filespath` is an absolute path into
the root that evaluated it, so a baseline from the base and a candidate from
an overlay would compare as a changed `filespath` and refuse the edit; five
comparators are called from about a dozen sites in `portedit`. The fix is
that `macports.Snapshot` carries the root it was evaluated in, set by the
evaluator and excluded from JSON, and the comparators normalize each side by
its own root, as `fidelity.Equivalent` already takes two. The host-access
helpers, `tolerateExplainedProbes`, `portGroupReadBenign`, and
`sourceBoundOperand`, already take the root of the observation they judge;
their callers pass the overlay's.

The batch session is bound to a root and refuses a context with another
(`eval/evaluator.go`, the `session is bound to` check), and it exists so the
probe's version loop and the survey's per-port probes do not start an
interpreter per candidate. The session accepts a context whose base root is
its own. That is correct because MacPorts resolves `_resources` from the
port directory upward and loads PortGroups per worker interpreter, so an
overlay evaluates in the base's session with the overlay's files; the
session's `sources` setting names the base, which the overlay is a copy of.

### Registry

```go
// Registry hands out one workspace per tree within a process and closes it
// when the last holder releases it. Overlays are not registered.
type Registry struct { /* repo, workspaces by tree, counts */ }
func (r *Registry) Acquire(ctx context.Context, source record.Source) (*Workspace, func() error, error)
```

`app` owns one registry per command. Its saving is one full materialization
per bump, not three: Tart staging and dependent discovery both work on the
prepared tree, so they share one workspace with each other and not with the
preparation, which worked on the base. The index closure in `app/selection.go`
keeps a `staged` map keyed by root for the same reason; it moves into the
registry keyed by tree, together with the assertion below.

### What writes into a root

The first draft said nothing writes into a workspace. Three things do, and
the design has to hold them:

- Index installation copies `PortIndex` and `PortIndex.quick` into the root
  with `O_TRUNC`. They are untracked, so an overlay never links them, and
  each root that needs the index gets its own copy. Installation becomes a
  write to a temporary name and a rename, so a concurrent reader of a shared
  workspace never sees a truncated index.
- Index generation sets every Portfile's mtime with `os.Chtimes`, which is
  the incremental protocol: unchanged Portfiles at the seed's time, changed
  ones a second later. That is inode metadata, shared through hardlinks, and
  the staging tar records mtimes. Index generation therefore keeps its own
  private full materialization, as `portindex.ensure` does today; it is the
  four-minute path on a cache miss and the three-second one from a seed, and
  the materialization is noise against either.
- `withContents` rewrites the Portfile in place; it goes away in step 3.

Files are created `0600`; the read-only property is a discipline, that
tracked files in a base are never opened for writing, and `Overlay` is the
only way to change contents.

### Blob store, later and measured

A content-addressed store beside the index cache, keyed by blob id, from
which a projection is hardlink creation. Two adjacent master trees differ by
a few hundred files, so the second full projection costs a walk, not 800 MB.
Not part of the first change; it is the answer if the survey's or staging's
full materialization shows up in a profile after the rest lands.

## The invariant, and what supports it

Evaluating target T in a workspace whose scope holds T's directory and
`_resources` yields the evaluation the whole tree would, provided the
evaluation reads nothing else in the tree.

The first draft said the evaluator's read interposer proves the proviso. It
does not, and cannot yet. The interposer is installed only for observations
with declarations (`observation_setup` runs only for an observation request,
and the `worker_init` trace only when declarations are on), so the baseline,
every candidate, name resolution, and the survey's evaluations run untraced.
Its `record` also drops any event with no Portfile frame, which is every
read from a PortGroup callback registered with `port::register_callback`,
and `glob` and `exec` events carry no path. Making it a proof needs three
changes that are worth making but are not this design's precondition:
tracing separated from declarations so it costs no `checkfiles` pass,
callback frames recorded, and enumeration paths captured. Until then a
sparse scope is supported by evidence and confined by policy.

The evidence, measured on the tree at 2026-09-21 with 20,107 Portfiles and
145 files, 9 symlinks, and 11 directories under `_resources`:

- No Portfile climbs above its directory: no `${portpath}/..`, no
  `[file dirname ${portpath}]`, and every `../` in a Portfile is a build
  path inside `worksrcpath` or a distfile path.
- No Portfile sources a tree file; every `source` is prose or `${prefix}`.
- No Portfile or PortGroup reads the PortIndex or another port at
  evaluation; `mportlookup`, `mportopen`, and `mportsearch` appear in
  neither. No PortGroup is loaded from a non-standard directory, and no
  Portfile overrides `filespath`.
- The two evaluation-time reads of tree content are both inside the port's
  own `files/` (audacity2's `file exists ${filespath}/...`), which the port
  scope holds.
- What Base itself reads from the root all lies under `_resources`: the
  PortGroup search path is the port directory's `../../_resources`, variant
  descriptions come from `_resources/port1.0/variant_descriptions.conf`, and
  the evaluator sources `_resources/port1.0/livecheck/*.tcl` on every
  evaluation. Base never reads the PortIndex to open a port by path;
  `mportinit` runs before the sources override and reads the host's
  configuration, which is outside the tree either way.
- The baseline survey's 41,730 evaluations recorded 700-odd host reads,
  every one "outside the captured ports tree".

The policy: a sparse scope is used only where one port is prepared and
nothing else is asked of the tree. Every consumer that resolves a bare
name, generates an index, evaluates arbitrary ports, or packs the tree gets
the whole tree, and the workspace asserts it: `Stage` refuses to generate an
index over a root whose scope is not `All`, and so does name resolution's
directory enumeration in `eval.Resolve`. A truncated index would otherwise
be indistinguishable from a tree with fewer ports, and it would be cached
under the tree id for every later process; that is the one failure that
would corrupt state outside the run, and the assertion is what closes it.
Installing an already cached generation into a sparse root is allowed, and
is how a bare-name `bump` on a warm cache stays sparse.

## What each consumer does

| consumer | today | with workspaces |
| --- | --- | --- |
| release resolution and preparation (`workflow/preparation`) | two full materializations of the source tree, one more of the candidate | one workspace from the registry, `EnsurePort` for the target once the name is resolved; candidates are overlays evaluated in the base's session; the final check evaluates the committed overlay, sparse |
| bare-name resolution before preparation (`app/selection.go` index closure) | stages the index into the materialized root | on a warm cache, installs the generation into the sparse root; on a miss, `EnsureAll` before generating, since the generator walks the root |
| candidate evaluation in `portedit` | `withContents` writes over the Portfile, evaluates, restores | `Overlay` per candidate; the probe's version loop overlays the Portfile only; observation reads immutable directories |
| assess and outdated (`survey`) | one full materialization, index staged, every port probed in it, 8 at a time | `EnsureAll` once, since the index needs it; each port's probe evaluates candidates as overlays of that one workspace; offline as today, which is a `nil` mirror at two call sites and worth a test |
| verify binding (`workflow/verification_bind`) | full materialization to resolve one target | `EnsureAll`, because resolving the name needs the index and a working-tree capture is a new tree every run |
| Tart staging (`verify/staging`) | full materialization, index, tar for the VM | the registry's workspace for the prepared tree, `EnsureAll`, same tar; edited overlay files keep their entries' modes so an executable stays executable in the guest |
| GitHub verification | no materialization | unchanged |
| dependent discovery (`macports/dependents`) | full materialization, index, evaluates many ports one interpreter each | the registry's workspace for the prepared tree, `EnsureAll`; its cost is the interpreters, not the files |
| index generation (`portindex.ensure` with no root) | private full materialization | unchanged, private, because generation rewrites Portfile mtimes |
| adopt, corrections, prepare-onto | read files through `repo.File`, bind through a full materialization, then the preparation path | unchanged, then as preparation |
| patch check and dependency generators | read `files/` under the port directory and extracted archives | `EnsurePort` covers `files/`; archives are outside the tree as today; `patches.go` reads `filespath` without the containment check `localPatches` makes, which is worth aligning |

A shared release edits one Portfile with its subports, one directory. A
future multi-target contribution ensures one port per target. A stub and its
carrier are one Portfile. Multi-profile observation, the one place that runs
several interpreters on one directory at once, gets simpler: it reads an
immutable overlay instead of writing once and reading many, and the
per-Portfile serialization in `assess` that exists because probing mutates
the file becomes unnecessary.

### Contributions that edit `_resources`

A candidate that changes a PortGroup is a normal overlay: `_resources` is in
every scope, so its evaluation sees the edited group, and the index treats
such a tree as a full pass through `requiresFullIndex`. But the contribution
flow refuses it before any workspace exists: a correction's scope check
(`workflow/verification_target.go`) refuses any changed path outside the
tracked port's directory, and `adopt` derives its port from the changed
directory. A working-tree verification captures staged files and refuses
untracked ones under `_resources/` until they are staged. So a branch that
edits a PortGroup together with a port is a limitation of the contribution
flow today, not of this design; it belongs on the roadmap as its own item,
and the workspace is ready for it when it lands.

## Sequence

1. `portedit/archives` split, unrelated to this and safe now.
2. Root identity: `Snapshot` carries its root, the comparators use it. A
   small change with its own tests, and a precondition for overlays.
3. `workspace` with `Open`, `EnsurePort`, `EnsureAll`, `Overlay`, `Commit`,
   the session that serves overlays, and the `All` assertions in `Stage` and
   `Resolve`; adopted by `workflow/preparation` and `portedit`, where the
   materializations and the in-place mutation live. `sourceInput` shrinks to
   the workspace, the target, the baseline, and the release inputs; the
   derived baseline in `dependencies.go`, today a shallow copy sharing the
   session, becomes its own overlay. Measured on a real bump before and
   after, and by the assess journal over the tree.
4. `portedit/observe` on top of the workspace: it takes a workspace, a
   target, and a platform, and observes immutable directories.
5. The registry, adopted by staging and dependent discovery, replacing
   `git.Snapshot` everywhere; index installation by rename.
6. The interposer as a proof, in three parts, and the blob store, each only
   if a measurement asks for it.

## Risks

- A Portfile that reads another port's files at evaluation would evaluate
  differently in a sparse workspace and, until the interposer is a proof,
  not say so. None exists in the tree today; the survey re-run over the
  sparse path is the check, and the policy keeps sparse to the one path
  where the Portfile being prepared is the one being evaluated.
- Hardlinked overlays require the base and the overlay on one filesystem;
  the workspace directory holds both. A tool that writes through a link
  would corrupt the base; the writers are enumerated above and none writes
  a tracked file.
- The preparation's final equivalence check exists to prove the in-place
  evaluation matched the committed tree. Overlays make the two the same
  bytes by construction; the check stays and evaluates the sparse overlay,
  which is cheaper, not weaker.
- A shared workspace is read by more than one consumer in one process, and
  each writes its own index copy into it; the rename makes that safe, and
  overlays are private. Close waits for the last release, and a workspace
  closes its sessions before its directory, which today is a `defer` order
  inside single functions.
- `EditTree` refuses a file that does not exist in the base, so an overlay
  cannot add a file; nothing prepared today adds one.
- An overlay costs a walk of `_resources` and the port directory, about 170
  links; the survey's probes would create a few per port. If that shows in
  the survey's time, a probe can reuse one private overlay for successive
  candidates, since its edited file is its own copy.

## Review

The first draft was attacked scenario by scenario by a second reviewer on a
different model, read-only, against the code and the tree; every finding
below was verified before it changed the text. Findings that held and what
they changed: the comparators' single root and the session's root guard
made overlays as sibling directories unworkable as drafted, and produced the
root-identity section; the interposer's observation-only installation and
its dropped callback frames turned "proven" into "supported by evidence and
confined by policy"; index generation over a sparse root would cache a
truncated index under the tree id, hence the `All` assertions; `Chtimes`
and `O_TRUNC` inside the root, and macOS hardlinking a symlink's target,
produced the writers section and the rule that overlays are built from tree
entries; the correction path's refusal of any change outside the target
directory moved `_resources` contributions from "supported" to "a
limitation of the flow, not the design"; the working-tree capture refuses
untracked `_resources` files rather than admitting them; the registry's
saving is one materialization, not three, since staging and discovery work
on the prepared tree. Findings recorded without changing the design: the
`patches.go` read of `filespath` without containment, the shallow-copied
baseline in `dependencies.go`, `outdated`'s always-wired HTTP client and
the absence of a test that `assess` makes no network call, `eval.Resolve`'s
category enumeration, and a Portfile in the tree requiring a PortGroup that
does not exist (`kde/qtcurve`, `kf5 1.1`).
