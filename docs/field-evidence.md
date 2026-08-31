# Field evidence

**Status: evidence, not proposal.** Nothing here is a decision, a recommendation, or a
requested design change. It is a record of what happened during two days of ordinary
MacPorts maintenance, written down because some of it may bear on questions the design
is currently holding open — and some of it may not. Where an observation seems to touch a
design question, that is noted as a question, not an answer.

Nothing in this file belongs in `decisions.md` unless and until the design conversation
decides it does.

## Where this came from, and how far it generalises

One maintainer's session against `macports-ports`, roughly fifty port updates plus
assorted fixes, conducted by an assistant driving `port` directly.

Known biases in the sample, all of which limit how far any of this travels:

- **Overwhelmingly Go ports.** A handful of Rust (`broot`, `nushell`, `ruff`), one C
  family (`skalibs`/`execline`/`s6`), one Zig-adjacent. Anything below about Go-specific
  idiom may simply not hold elsewhere.
- **A maintainer with commit access.** Most of the session's mechanics — direct commits
  to `master`, six ports in one push, a 192-file sweep as one commit — are unavailable to
  the contributors dockhand would most help. See "PR-shaped constraints" below, which was
  raised late and is the least explored area here.
- **One maintainer's ports**, so idiom is more consistent than the tree at large.
- **Observed through one executor.** What got noticed is what that executor happened to
  check. Absence of evidence here is weak evidence of absence.

## 1. Build success did not imply the intent succeeded

Four ports compiled, linked, destrooted and passed `port lint` while the thing the update
was *for* had silently not happened:

| port | version intended | binary reported |
|---|---|---|
| `termshot` | 0.6.1 | `(development)` |
| `dblab` | 0.48.1 | `version unknown` |
| `atlas` | 1.3.0 | `community version - development` |
| `mage` | 1.17.2 | `Mage Build Tool (devel)` |

In each case the cause differed — no ldflags passed at all; ldflags passed to a Makefile
that ignored them; upstream moving version reporting to `debug.ReadBuildInfo()`, which
yields `(devel)` for a tarball build. What caught all four was the same cheap act:
running the built artifact and reading its self-reported version.

*May bear on:* whether the verification ladder's rungs are ordered by cost alone, and
whether a `Bump` can be considered verified by a build. Possibly nothing, if verification
depth is expected to be chosen per-intent by a human anyway.

## 2. Most failures originated upstream, not in the Portfile

The Portfile edits were nearly all trivial. The failures were not, and their causes were
invisible in the Portfile at plan time:

| port | what moved between versions |
|---|---|
| `skopeo` 1.22→1.24 | module renamed `github.com/containers/skopeo` → `go.podman.io/skopeo` |
| `goimapnotify` 2.4→2.5 | `package main` moved to `cmd/`; upstream began shipping `vendor/` |
| `wails` 2.3→2.15 | root module removed; CLI now under `v2/` |
| `hermit` 0.38→0.52 | `Makefile` replaced by `Justfile` |
| `mage` 1.15→1.17 | `magefile.go` → `magefiles/`; version moved to `ReadBuildInfo` |
| `mp4ff` 0.33→0.56 | version symbol `mp4` → `internal`; 4 binaries became 8 |
| `gum` 0.17→2.0 | module moved to `charm.land/gum/v2` |
| `scc` 3.7→4.0 | module `/v3` → `/v4` (turned out to need no Portfile change) |
| `dalfox` 2.12→3.2 | **rewritten from Go to Rust**; no `go.mod` at all |
| `kubeconform` 0.6→0.8 | began shipping a complete `vendor/` |
| `vsh` 0.14→1.0 | began shipping `vendor/` |

A `Bump` on `skopeo` is one literal on one line. By any Portfile-shaped measure it is the
simplest possible edit, and it was wrong.

*May bear on:* whether a tier computed from `(intent, port)` is sufficient, or whether the
target version is a third input. Note this may be a Go-specific artefact — Go's module
path is unusually load-bearing, and other ecosystems may not move like this.

## 3. Checks that would have predicted §2, mechanically

Each of these was performed by hand, repeatedly, and each caught real breakage:

- `go.mod` module path, current tag vs target tag
- location of `package main`
- presence of the build driver the Portfile invokes (`Makefile`, `build.sh`, `make.bash`)
- `patch --dry-run` for every patchfile
- whether `-X` ldflag targets still exist as package-level vars
- whether `vendor/` appeared or disappeared
- whether the language changed at all

*May bear on:* whether something like this belongs in `classify`. It is cheap and needs no
evaluation. It is also entirely Go-shaped as written, and the general form — "diff the
structural assumptions the Portfile makes against the new upstream tree" — is unproven.

## 4. The authoritative verifier was remote and late

Three ports built locally and failed on the MacPorts buildbot, discovered after merge:

- `usql` 0.21.4 — dependency `cockroachdb/swiss` does not compile under Go 1.27. Local
  `go` had been 1.26 at test time; the builders had 1.27. Fixed by pinning the port to
  Go 1.26 via `PATH`, because `build.sh` calls bare `go`.
- `skopeo` — the module rename in §2, which local caches masked.
- `mox` — `go tool sherpadoc` requires module mode; the port built offline.

Also relevant: `orbiton` appeared to have regressed from a change I had made, and the
failures turned out to date from January. Distinguishing "my change broke this" from
"this was already broken" required per-builder history.

*May bear on:* whether the pipeline ends at `promote`, and whether findings can originate
outside the local machine. Under a PR workflow this loop is tighter — CI runs before merge
— so this may be more a committer's problem than a contributor's.

## 5. A failure attributed to the wrong port

`copilot` failed to build. `copilot` was fine. Its build dependency `packr` had been
broken independently — its Makefile's `build` target depends on `deps`, which runs
`go get`, impossible under the port's offline build. Nothing else built `packr`, so it had
sat broken unnoticed.

*May bear on:* whether a finding is always attributable to the port under intent.

## 6. One change that had to span three ports, in order

`skalibs` records its configure target in `lib/skalibs/sysdeps/target`; `execline` and `s6`
compare it verbatim. The recorded value contained the kernel *point* release, so any macOS
update broke all dependents. The fix had to touch all three ports and `skalibs` had to be
rebuilt first, or the consumers still mismatched. Filed as
[#74350](https://trac.macports.org/ticket/74350).

This is not a sweep — a sweep is N independent applications. It is one change with an
ordering constraint across three ports.

*May bear on:* whether point and sweep are the only two shapes.

## 7. A build-tool bump silently raised a floor for six dependents

`goreleaser` 2.18.0 requires Go 1.27. Six ports build-depend on it while declaring less:
`gh-dash` (1.25.8), `trivy` (1.26.3), `grype` (1.26.3), `confluent-cli` (1.26.5), and
`clef` and `oui` which declare nothing.

Nothing breaks today, but only by luck: Go 1.25, 1.26 and 1.27 share a `min_darwin` of 21.
Had 1.27 raised that floor, those six would have been attempted on systems where
`goreleaser` cannot build, and failed at dependency-install.

*May bear on:* whether a toolchain constraint propagates across `depends_build`, and
whether `go.toolchain_min` describes a port's own `go.mod` or its effective requirement.
The second reading is not obviously correct — the portgroup documents the first.

## 8. The "needs updating" signal was unreliable in both directions

Repology reported updates that did not exist for at least these:
`websocketd`, `py-sdnotify`, `jtc`, `certgraph`, `go-mockgen`, `unison-lang`, `xs`,
`xmake`, `golines`, `lore`, `oasdiff`, `pup`, `goful`, `pomo`, `gdrive`, `infracost`.

And it under-reported elsewhere: `fyne` (said 2.6.1, actual 2.8.0), `goplus` (1.6.2 vs
1.7.5), `mp4ff` (0.55.0 vs 0.56.0). For `zot` it reported 2.1.20, which belongs to a
different project entirely; the real target was 0.3.48.

Every one of these cost a `port livecheck` to establish, and the conclusion is not
recorded anywhere in the ports tree.

*May bear on:* D8, if a conclusion that a port does **not** need work is worth persisting.
It may not be — the cost of re-deriving is one livecheck.

## 9. Revision bumping was a decision procedure

Observed rules, each of which came up:

- version changed → `revision` resets to 0
- build fixed at the same version → increment (`skopeo`, `mox`, `usql`, `gitea-tea`)
- whitespace only → none (`telegraf` realignment)
- gating annotation added → none (`go.toolchain_min`, 192 ports)
- distfile source changed, content byte-identical → none

The last needed evidence: switching `github.tarball_from tarball` to the portgroup default
changes the distfile name, size and checksums. Diffing the extracted trees showed zero
content differences — only the root directory name and 24 bytes of tar metadata — and the
portgroup relocates the source to `gopath/src/${go.package}` regardless, so the build path
is unchanged.

*May bear on:* whether `BumpRevision` is a T0 edit or a judgement with several inputs.

## 10. Patch rot had a third disposition

`mage` and `mp4ff` both carried patches that no longer applied. Neither wanted dropping
(the patch's purpose was still needed) nor refreshing (the anchor was gone). Both needed
the patch's *semantic intent* re-derived against a restructured tree — for `mp4ff`, the
version symbol had moved package and the target list had doubled; for `mage`, the patched
file had moved directory and the mechanism it patched no longer existed.

*May bear on:* whether `DropPatch` and mechanical patch refresh (now a `Bump` capability; `RefreshPatches` struck 2026-08-30) cover the space.

## 11. PR-shaped constraints (least explored)

Raised late in the session and not tested in practice, so this is the weakest section here.
The maintainer running the session has commit access; most contributors do not, and for
them everything routes through a pull request against `macports-ports:master`.

Observations that may follow, none verified:

- The remote topology is a triangle — upstream (never a push target), the contributor's
  fork, local. This session used it for five PRs to other maintainers' ports.
- A merge commit inside a PR branch is a routine review complaint; this session had to
  discard one that GitHub Desktop introduced, and rebase twice more.
- The `maintainers` field encodes permission, not just an address: `nomaintainer`,
  `openmaintainer`, or neither, the last implying consent is expected first.
- One PR per port is the norm, because different maintainers review different ports. This
  sits awkwardly with §6 (one ordered change across three ports) and with sweeps — the
  192-file annotation commit is unreviewable as a single PR and antisocial as 192.
- The PR template asks for `Tested on` and a verification checklist. A tool knows exactly
  which rungs it ran, so it could fill this truthfully; this session hit the tension
  directly, ticking "tried a full install" with an appended note that only `destroot` had
  run, rather than leaving it bare or claiming it outright.
- MacPorts notifies maintainers automatically, so manual `@`-mentions are redundant and
  `[skip notification]` is an opt-*out*.

## 12. Absent from the catalogue: creating a new port

The ten intents are all maintenance of an existing port. Nothing covers producing a new
one, which is plausibly the hardest task for the newcomers a PR workflow implies, and the
one with the most unwritten convention attached.

Whether this is an omission or a deliberate scope boundary is not clear from the documents.
It may fail the "nameable end state" admission test — though "a working port for upstream X
at version Y" is arguably nameable.

## What this session tends to support

Offered symmetrically, since the sections above are mostly about gaps:

- **The read/write invariant held.** Every case encountered fits `plan` reads the world /
  `apply` writes the worktree / `promote` writes elsewhere. Nothing wanted to violate it.
- **Refusal is genuinely a feature**, and was needed more often than expected — `dalfox`
  (language rewrite), `gomuks` (changed identity), `mergestat` (beta only). All three were
  correct outcomes, not failures.
- **`RefreshChecksums` as a supply-chain trap is right.** The benign version occurred:
  `gitea-tea`'s distfile was renamed by a portgroup change while its content stayed
  byte-identical. Establishing that it was benign took a deliberate re-fetch and
  comparison — exactly the human moment that entry predicts.
- **`classify` looks like the highest-value early command.** A large share of this session
  was classification wearing other clothes.
