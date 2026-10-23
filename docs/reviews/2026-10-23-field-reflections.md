<!-- Field testing's reflections after some 300 bumps, copied whole from the project's files on 2026-10-23. The roadmap's Then coalesces them, with architecture's weighing beside this file; where they differ, the roadmap's decides. -->

# dockhand after ~300 bumps: gaps, improvements, and scope

From the field-testing thread, 2026-10-02 to 2026-10-23: roughly 300 PRs on macports/macports-ports, from rc9 through 0.4.0 and main. Each point below names the ports where it actually came up. Where I'm inferring rather than observing, I say so.

## Where the 2026-10-09 wishlist stands

| # | Item | Now |
|---|---|---|
| 1 | One command from a port list to PRs | **Done.** `bump`, `ship --mine`, `ship --passing`. |
| 2 | A supported path when update refuses | **Still the biggest gap.** See A below. |
| 3 | Knowing a submitted PR went red, and why | **Partly done.** status shows CI ✓/✗, but not the cause (see F). |
| 4 | Checks that match MacPorts CI | **Done.** Build profiles, minmax, dependency isolation. |
| 5 | Policy-driven holds | **Done.** Majors, repo moves, raised floors, upstream-comparison findings. |

## Gaps, ranked by how much of my time they cost

### A. Refusals I resolved mechanically, every time

This is still the biggest time sink. dockhand refuses when it can't prove an edit is safe, and then I make that same edit by hand in a couple of minutes:

| Port | Refusal | What I did by hand |
|---|---|---|
| xapian-core | three distfile families under one version | version and three checksum blocks |
| git-credential-oauth (and cargo ports before 32427a61) | "checksums set by the PortGroup" | nothing to regenerate: the vendor list came out identical |
| purl | Cargo.lock at the workspace root, above worksrcdir | cargo2port on the root lock |
| tfvar | go.vendors spelling (`/v13`) taken for an override | go2port |
| fresh (Link Dupont's report) | post-patch edits a Cargo.toml | the lock is still a valid superset |
| LibreCAD | qmake5 and boost probe host state | version and checksums |
| libxkbcommon | master_sites directory now 404s | `github.tarball_from archive` |
| pyinfra | build.pre_args embeds the work path | version and checksums |

Several of these are fixed now, but the pattern keeps producing new cases.

**Proposal: "attempt, then let the check decide."** When dockhand's confidence is low but the edit is mechanical, it would make the edit anyway, mark the branch "unverified edit: <reason>", and require a passing check before submit. That would fold most of the cases above into the normal flow. It doesn't conflict with "never guess": the check is the proof, and nothing reaches a PR without it. `checksums` already moves in this direction.

### B. Patch upkeep

| Port | What happened |
|---|---|
| e2fsprogs | patch obsolete; upstream dropped the code |
| net-snmp | offset after an autoconf rewrite |
| rsync | stale hunk in a generated man-page header |
| confluent-cli | trailing-newline hunk fixed upstream |
| duckdb | CMake file reformatted |

dockhand reports these before the check, and it handles the "already upstream" case (it detects reversed patches and drops them, as it did for net-snmp's patch-legacy-compat.diff). Every other repair was by hand.

**Proposal: a `patches` helper.**
- For each failing patch, try fuzz and offset first.
- Drop a hunk that's already present upstream.
- Flag a hunk that only touches generated files (man pages, configure output).
- Regenerate the patch from an applied tree, keeping its header comment.
- Show the old and new patch side by side for review.

This is mechanical work, and it's well within dockhand's style.

### C. Build-configuration drift: good, with a blind spot

The upstream comparison earned its keep. It caught i2pd's C++20, duckdb's removed options, rsync's IDN default, justpath's setuptools switch and relicense, and vault's go.mod floor.

**The blind spot:** whatever is decided at configure time and doesn't show in the source diff. Examples:
- duckdb's `LINK_CORE_EXTENSIONS` silently dropped two extensions; only the port's smoke test caught it.
- libxkbcommon's optional ICU.
- e2fsprogs' new libarchive pkg-config lookup.

**Proposal: diff the configure summary.** Keep the old version's configure/CMake summary from its last check, and diff it against the new one. That covers found and not-found dependencies, enabled features, and lines like "built but not linked". This is where opportunistic dependencies and dropped features show up.

**False positives seen:**
- GraphicsMagick's "library X removed" list (fixed);
- libxkbcommon's SPDX-labelled LICENSE read as an unknown license;
- vault's licensor rename;
- ratty's "binary removed" (fixed).

The license matcher could compare license identifiers rather than raw text.

### D. Failures that aren't the port's fault

Much of my diagnosis time went here:

| Failure | Cause |
|---|---|
| Monterey libgcc15 | the image's Xcode 14.0.1, which gcc15 refuses |
| SourceForge "HTML page" | the Referer header on redirects (fixed) |
| proxy.golang.org stream reset | network blip (retry fixed) |
| guest APFS resize | flaky; now retried |
| vifm's IPC test, Crystal's suite | test environment inside the VM |
| py310-docutils | broken on master by an unrelated py-roman commit |

**Proposal: an automatic baseline on new failures.** When a release fails, rerun that one target at master before reporting, then classify the failure as infrastructure, environment or image, a dependency failure, or a port regression. `--baseline` and `--accept` exist, but I ran them by hand every time. The minmax oldest-release image (Monterey) will keep producing environment-class failures.

**Also: the image's Xcode.** If MacPorts' own buildbot can't build something on that Xcode, dockhand could say so up front. The fact that packages.macports.org has no archive for a port on darwin_21 is already a strong signal.

### E. Dependents and ABI

dockhand is port-local, but a third of the hard cases weren't:

| Case | What was missed |
|---|---|
| exiv2 0.28 | soname change; 38 dependents |
| raylib | SOVERSION bump; no dependents |
| harfbuzz | this maintainer updates `-devel` first |
| brotli | I inferred, rather than checked, that its soname stayed |

The link reading from part 6 already gets the information needed.

**Proposal: soname diff plus revbump suggestions.** After a library port's check, compare its built dylibs' install names and compatibility versions against the previous version's. If they changed, list the dependents (`impact`) and offer the revbump set. That turns "I think dependents are fine" into evidence.

The link check also has a gap: it skips targets whose tests fail as advisory (Crystal). Those builds are still what ships.

### F. After submit

| Case | What happened |
|---|---|
| wasmedge | merged before my follow-up fix; needed a fresh branch, done by hand |
| xapian-core | CI red, with the cause only in the job log |
| stale cards | merged PRs pinned in the thread; no way to remove them |

**Proposals:**
- When a branch's PR has merged and the branch has new commits, `ship` could offer a follow-up branch, carrying the commits over onto master.
- status could quote the failing CI step's error line (see wishlist #3).

### G. Ergonomics that cost real time

- **Running alongside other sessions.** Several sessions share one DB, and an installed 0.4.0 would be locked out by a migration. The DB-copy workaround worked, but status could warn about it, listing the live instances, their builds and schema compatibility.
- **Long runs.** Checks outlive an agent's tool timeout, so I wrapped every batch in nohup scripts. `check -d` now starts its own drain, which should help.
- **`tidy` skips untracked files.** On net-snmp this cost two VM runs.
- **`path` outside the tree.** It errors when run from outside the ports tree.
- **`outdated` against the GitHub quota.** Over 2,133 ports it would have overshot the hourly quota. That's now on the roadmap.

## Where scope could reasonably grow

These are ordered by how much I think they'd pay off.

1. **Per-port maintainer policies.** `holds.majors` is global. Real practice is per-port and per-maintainer: "update -devel first", "skip prereleases", "follow repo moves", "never auto-submit majors for X". A small policy file, or Portfile-adjacent metadata, would let unattended runs respect each maintainer's way. harfbuzz is the concrete case.
2. **Series management.** "Add the 1.17 series" for terraform, go, helm, zig and similar was hand work: copy a subport block, then update latestVersion and the placeholder. It's a frequent, mechanical, convention-heavy task, a good fit for a command like `dockhand series add terraform 1.17`.
3. **Tree-health signals during bumps.** py310-roman and docutils, libgcc15 on Monterey, and pgdog's missing darwin_21 packages are all visible from packages.macports.org and the port index. Reporting "this dependency is broken at master" separately from "your port failed" would save the most diagnosis time. Opening a Trac ticket draft for such breakages is a natural next step.
4. **Upstream patch sourcing.** Several fixes already existed elsewhere: FreeBSD's rsync patch, upstream commits for exiv2 consumers. A lookup for a failing patch or build error across FreeBSD ports, Homebrew and the upstream repo's newer commits would be a search, not a guess, and would keep dockhand's "official sources only" stance.
5. **Smoke-running installed binaries.** `foo --version` would have caught duckdb's missing extensions without a port test. This is on Later per "core capabilities first"; I'd keep it there, but it's the cheapest strong correctness signal left.

## What worked well enough to keep as-is

- The bump loop for Go and Rust ports: most updates went version to PR with no intervention.
- Holds with reasons.
- Build profiles.
- Per-subport reuse.
- `--accept` against a baseline.
- The upstream comparison as a whole.
- The PR description's build matrix.

These changed how fast I could work; I'd protect them in any redesign.
