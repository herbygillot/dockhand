# What MacPorts reviewers ask for that no tool catches

A survey of human review feedback on merged pull requests in `macports/macports-ports`, compared against what dockhand (main, 7a1fa6e) already enforces. Written 2026-10-02.

## The sample

- 140 merged PRs, closed between 2025-01 and 2026-10, read in four slices:
  - PRs that had a changes-requested review, May to October 2026 (37 PRs)
  - PRs that had a changes-requested review, 2025 to April 2026, the 30 with the most discussion
  - new-port submissions with human discussion (48 PRs)
  - update and fix PRs with human discussion (29 PRs)
- For each PR, the inline review threads, review bodies and conversation were read. Bots and CI chatter were skipped.
- The result is 566 pieces of feedback, each one a rule, convention or best practice rather than one-off debugging.
  - Every item is recorded verbatim with its PR number in `survey-records.jsonl`, kept in the Dockhand project files (review-survey/).
  - Each item is tagged by whether a tool could detect it from the Portfile, the diff, the commit or the PR text: 303 fully, 202 partly, 61 not at all.
- reneeotten wrote 46% of the feedback. herbygillot wrote 15%, barracuda156 11%, ryandesign 6% and jmroot 4%.
  - That concentration is the strongest argument for automating this: a handful of people repeat the same comments.

## Already covered by dockhand

These came up often in the survey but dockhand already handles them, so they are not proposed again.

- **Checksum types.** dockhand rewrites md5/sha1 checksums to rmd160/sha256/size.
- **Commit structure:**
  - `port: subject` format (`subject-port`)
  - vague subjects
  - squashing fix-up commits (`follow-up`)
  - no merge commits
  - bare `#NNNN` ticket references
- **Revision reset to 0 on a version update.** This is both an automatic edit and the `revision-after-update` rule.
- **HTTPS homepage and master_sites.** dockhand gives a notice for plain-HTTP URLs, and `create` writes the https form.
- **`create`'s layout:**
  - modeline
  - column-20 alignment
  - `revision 0`
  - SPDX licences mapped to MacPorts names
  - category validity
  - name collisions
- **Stealth updates.** dockhand handles `dist_subdir`, `go.vendors`/`cargo.crates` regeneration and `go.toolchain_min`.
- **Upstream licence and dependency changes** on update are covered by the `assess` rules.
- **Other open PRs for the same port** are listed.

## Gaps worth enforcing

Each gap below is common, mechanical, and has a low false-positive rate. The counts are distinct PRs where reviewers raised the issue (out of 140).

**One principle runs through all of these: flag only lines the change touched.** In #32359, ryandesign told another reviewer not to burden contributors with lines their PR didn't change. In #28273, barracuda156 called cosmetic reordering optional. A rule that reports pre-existing problems in someone else's port will be resented.

### 1. Lines that restate what a PortGroup or base already sets (35 PRs, 63 items)

This is the most common Portfile comment in the survey.

- **What reviewers flag:**
  - `name`, `version`, `homepage`, `master_sites` or `distname` after `github.setup`, which sets them already.
  - The same after `go.setup`.
  - `use_configure no` under the cargo PortGroup.
  - An explicit `py-setuptools` or `py-build` dependency, or `python.pep517 yes`, under the python PortGroup.
  - `port:go` under golang.
  - `use_parallel_build yes`.
  - A `v` written into the version instead of as the tag-prefix argument of `github.setup`.
  - Duplicate lines.
- **Examples:**
  - #33490 "`name` and `version` are implicitly set by the `github.setup` line above"
  - #28024 "this is the default from the `python` PortGroup so please remove"
  - #31382 "the `v` should not be part of the version string, but is the 'prefix'"
- **How a tool could check it:**
  - For each option line the diff adds, evaluate the Portfile with and without that line. If the option's value is identical, the line is redundant.
  - dockhand's fidelity evaluation already compares evaluated option values, so this reuses existing machinery and needs no hand-kept list of PortGroup defaults.
  - The `v`-prefix case is a text check: the `github.setup` version starts with `v` and the tag-prefix argument is empty.

### 2. Revision bumps that were unnecessary, or missing (21 PRs)

dockhand resets revision to 0 on an update. Reviewers more often correct the opposite mistake: a contributor bumping the revision when nothing installed changes.

- **No bump needed:**
  - When only adding to `python.versions`, since that adds a subport and changes nothing for existing ones (#34703, #34778).
  - For build-only fixes (#34700, #29711).
  - For build-dependency changes (#34023).
- **A bump is needed:**
  - When `depends_lib` or `depends_run` changes, or installed files change (#34094, #31821, #33925).
  - When a port is obsoleted with `replaced_by` (#32451).
- **Never** remove an `epoch` (#32263).
- **Proposed rule:** classify a revision-only diff.
  - Warn when the revision rises but the diff only touches `python.versions`, `depends_build`, `depends_fetch`, `depends_extract`, patches gated to a phase, or build flags.
  - Warn when `depends_lib` or `depends_run` changes with no revision bump and no version change.
  - Make removing an epoch line an error.

### 3. Commit and PR structure that dockhand doesn't check yet (about 20 PRs)

dockhand covers subject format and squashing. These are the remaining gaps.

- **One Portfile per commit** (6 PRs). "each commit only touches one Portfile" (#27148). tidy notes a commit split across port directories; submit could make this a commit-rule warning.
- **Trac tickets as a `Closes: https://trac.macports.org/ticket/N` trailer in the body, not in the subject** (6 PRs). "The Trac ticket should not be in the short summary" (#31382). Also, "Closes: #PR" for superseded PRs (#31534). The current `ticket-url` rule catches bare `#NNNN` only.
- **Whitespace-only changes in their own commit** (3 PRs). "As usual: whitespace changes in different commit please" (#34683, #34027). Detect a commit whose diff mixes whitespace-only hunks with functional ones.
- **No unrelated changes to a port you don't maintain** (5 PRs). Examples: reformatting, or rewriting a working livecheck. Make this a notice when the submitter is not a maintainer and the diff touches lines unrelated to the version or fix.
- **PR title matches the commit subject** for single-commit PRs (#27718). dockhand writes the title, so this may already hold. That is inferred, not verified.

### 4. `github.tarball_from` (10 PRs)

- Every github-PortGroup port should set it explicitly (#27115, #30692).
- `tarball` is deprecated in favour of `archive`.
- Prefer `releases` when upstream publishes a release asset, since it usually ships a generated `configure` and so avoids `use_autoreconf` (#32359).
- Change it only together with a version bump, otherwise the checksum changes under the same version (#31382).
- **Gap in `create`:** it always writes `github.tarball_from archive` (`internal/macports/newport/newport.go:217`) and `use_autoreconf yes` for autotools projects.
  - It should check for a release asset matching the tag and prefer `releases` when there is one.
  - For edits, warn on `tarball`, and on a `tarball_from` change without a version change.

### 5. Path-style and correctly typed dependencies (19 PRs)

- **Use path-style where several ports can satisfy a dependency** (8 PRs):
  - `path:bin/pkg-config:pkgconfig` so `pkgconf` satisfies it (#28171, #32359).
  - `path:lib/libopenblas.dylib:OpenBLAS` so `OpenBLAS-devel` does (#34029).
  - doxygen, gettext, and any port with a `-devel` sibling.
- **Mechanical rule:**
  - Warn on `port:X` when the index has `X-devel`, or X is a known multi-provider such as pkgconfig, gettext or OpenBLAS.
  - Warn on a dependency whose name doesn't exactly match an existing port, including case (#34029, #29631).
- **Dependency type:**
  - `pkgconfig`, build backends and code generators belong in `depends_build`, not `depends_lib` (#27663).
  - This is partly mechanical: a short list of known build-only tools.

### 6. `-append` instead of a bare set (7 PRs)

- A bare `depends_build`, `depends_lib`, `configure.args` or `build.args` overwrites what the PortGroup put there. "the `meson` PortGroup adds build dependencies, so you should use here `depends_build-append`" (#27663).
- **Mechanical rule:** evaluate the option with the PortGroups loaded but before the Portfile's own assignment. If it was non-empty and the Portfile assigns it without `-append`, warn.

### 7. Python versions (13 PRs)

- **Gap in `create`:** it writes `python.versions 313` (`internal/macports/newport/newport.go:273`). Reviewers now ask for 3.14.
  - "this should use Python 3.14 if at all possible" (#34810, #34778).
  - For new libraries: "New ports should only add 313 and 314 by now unless earlier versions are needed" (#29930).
  - The version should come from the python PortGroup's `python.default_version` and the newest supported version, not a constant.
- **Applications versus libraries:**
  - An application should drop the `py-` prefix and use `python.default_version`, not `python.versions` (#33801).
  - A new `py-*` dependency added for one application should carry only the subport that application needs (#34778, #34690).
- **Edits:**
  - Warn when adding an end-of-life Python version.
  - Give a notice listing existing versions that nothing depends on (`port echo depends:py39-x`, #29930).

### 8. Licence names on edited Portfiles (8 PRs)

- SPDX spellings such as `BSD-3-Clause` are rejected: "this isn't a valid license for MacPorts, it should remain 'BSD'" (#34307, #34265, #34281).
- `create` already maps these. Edits get no such check, and `port lint` doesn't catch them. A contributor in #30045 was "surprised that `port lint --nitpick` didn't catch that".
- **Proposed rule:** run each licence token on a changed `license` line through the existing SPDX map (`internal/macports/license.go`). Warn and suggest the MacPorts name.

### 9. Maintainers (14 PRs)

- `openmaintainer` alone is invalid; it should be `nomaintainer` (#34753).
- A non-committer must use the obfuscated-email or `{domain:user @handle}` form. A bare handle is "only for members with commit access" (#27901, #35027).
- On a new port, reviewers ask the submitter to maintain it themselves, optionally with `openmaintainer`, rather than leaving `nomaintainer` (#32336, #27901).
  - dockhand already suggests a line. It could add a notice that reviewers expect the submitter to take it.
- Coupled or sibling ports, such as yt-dlp and yt-dlp-ejs, should share maintainers (#30045). This would be a notice.

### 10. `platforms` with `supported_archs noarch` (10 PRs)

- A `noarch` port needs `platforms {darwin any}`, or `any` for non-Python ports. "it installs as a Framework on darwin" (#34621, #34629).
- An existing `platforms any` should stay (#34519). There, a contributor removed it because of a `port lint` warning they had carried over from another port.
- **Mechanical rule:** `supported_archs noarch` without `any` in `platforms`.

### 11. Dead code and stray files (9 PRs)

- "please remove the line instead of commenting out" (#32161, #29636).
- A stray Portfile copy inside `files/` (#34072).
- **Mechanical rule:**
  - Warn on added lines that are commented-out Tcl. A heuristic: a `#` line whose remainder parses as an option or a known command.
  - Warn on files in `files/` that no Portfile line references.

### 12. Whitespace and layout on changed lines (20 PRs)

- Indentation in multiples of four spaces.
- Continuation lines aligned with the rest of the Portfile (#27115, #28171).
- A modeline on line 1 (#29555).
- No trailing whitespace.
- No `\` continuation that swallows the next option (#27718, #30045).
- **Gap:** dockhand writes these correctly in `create`, but nothing checks an edited Portfile. Scope the check to lines the diff adds.

### 13. Surface `port lint` warnings, and run `--nitpick`

- The check environment runs plain `port lint` and fails only on errors (`internal/buildenv/tart/guest.tcl:350`). Warnings are not shown, and `--nitpick` is never run.
- Reviewers flagged two things lint had already warned about but the contributor missed:
  - unknown dependencies in new subports (#30051)
  - an autoreconf warning (#28116)
- Showing lint and nitpick warnings for every changed subport, as notices, is cheap. Running lint in `review` is already on the roadmap (Batch 11 leftover).

## Worth a notice, not a rule

These came up often but need judgement, or detecting them is only partial.

- **Use the fitting PortGroup or helper** (23 PRs):
  - golang rather than github for Go projects (#34177)
  - meson (#30935), makefile, openssl (`openssl::lib_dir`), legacysupport
  - `${go.bin}`, `cargo.offline_cmd`
  - Signals to detect: the github PortGroup with `build.cmd go build`; `use_configure no` with a `make` build; a `meson.build` upstream without the meson PortGroup.
- **Compiler standard rather than blacklists or flags** (17 PRs):
  - Use `compiler.cxx_standard` / `compiler.c_standard` instead of `-std=` in flags or manual clang blacklists (#34321, #27711).
  - Read the standard from upstream's `CMAKE_CXX_STANDARD` or meson's `cpp_std` and suggest it.
  - Flag `-march=native` (#30445) and builds that ignore `configure.cc`.
- **Undeclared or opportunistic dependencies** (12 PRs). These are found by trace mode (`port -t`) or `otool -L`, not from the Portfile.
  - dockhand's clean check environment already avoids the "it built because Qt5 was installed" case (#32263).
  - Comparing `otool -L` on the destroot against `depends_lib` would catch undeclared libraries mechanically.
- **Dependents on a library update** (10 PRs). When an installed dylib's install name or compatibility version changes, dependents need a revbump (#30773).
  - Revbump policy is applied case by case: ryandesign (#32057) and reneeotten (#33970) both pushed back on blanket revbumps.
  - So the tool should report the ABI change and list dependents, not bump them automatically.
- **Patches** (23 PRs):
  - Each patch needs a comment and an upstream link (#31352, #32336).
  - Apply patches only where they are needed (#34072).
  - Prefer a patchfile for fixed edits and `reinplace` for prefix substitution.
  - Never hard-code `/opt/local`: use `@PREFIX@` plus `reinplace "s|@PREFIX@|${prefix}|g"` (#27711, #30806).
  - On update, flag workarounds that may now be stale.
  - Mechanical parts: patch files with no header comment or URL; `/opt/local` in added patch or Portfile lines.
- **Build-from-source policy** (13 PRs):
  - No prebuilt upstream binaries (#29551, #31178).
  - No network during the build; use `go.vendors` or `cargo.crates` (#29551, #32556).
  - No Apple system tools such as `/usr/bin/python3` (#34321).
  - No `fetch.type git` when a tarball exists, because tarballs can be mirrored (#33595, #34015).
  - Reproducible builds with no timestamps (#27999).
  - Mechanical parts: `/usr/bin/` tools in added lines, `fetch.type git`, binary distfiles.
- **Latest upstream version** for new ports and updates (10 PRs). Prereleases belong only in `-devel` ports (#29187, #31630).
- **Variants** (14 PRs):
  - Avoid `no_*` variants (#30045).
  - Avoid variants that install nothing.
  - Keep variant-only dependencies inside the variant (#30594).
  - The default variant must build (#28809).
  - Optional variants are never built by the buildbot (#34087).
- **OS-version logic** (6 PRs):
  - Guard `os.major` with `os.platform darwin` (#29706).
  - Darwin numbers no longer trail macOS by one: macOS 27 reports Darwin 27 (#33571). Reviewers still correct the older mapping too, e.g. "Darwin 24 is macOS 15" (#31297).
  - Use `minimum_xcodeversions` (#31297).
  - Mechanical parts: an unguarded `os.major` comparison; a comparison that assumes the old Darwin-to-macOS offset at 26 or above.
- **Obsoleting and renaming** (14 PRs):
  - Use the obsolete PortGroup with `replaced_by`, a revision bump, and a comment saying when it can be removed. The comment is not a `notes` entry (#34281).
  - The unversioned port follows the latest release; older lines get versioned names (#34753).
  - Declare `conflicts` in both directions (#31297).
- **Testing before submitting.** A few PRs were plainly untested (#29930, #29964). dockhand's check gate already covers this.

## Contested: do not enforce

- **Legacy macOS support** (11 PRs).
  - reneeotten asks for a fallback version for old systems (#29706, #29310).
  - pmetzger (#33053) and drkp (#31178) say not to hold up updates for it.
  - At most, give a notice when an update raises the minimum macOS or drops an architecture, and leave the call to the maintainers.
- **Revbumping dependents.** "Justify each revbump" is the only consistent position. See the dependents notice above.
- **Cosmetic changes to lines a PR didn't touch.** Reviewers disagree, and this is why every rule above is scoped to the diff.

## Suggested order

1. **Small changes to `create`:**
   - Python version from the PortGroup, not 313 (item 7).
   - `releases` when a release asset exists (item 4).
2. **Diff-scoped Portfile rules on the existing commit-rule path**, so tidy, submit and review all get them:
   - redundant lines under PortGroups (1)
   - `-append` (6)
   - licence names (8)
   - noarch platforms (10)
   - maintainers form (9)
   - `tarball_from` (4)
   - path-style for known providers (5)
   - commented-out code and stray files (11)
   - whitespace on added lines (12)
3. **Revision-diff classifier** (2) and commit-structure warnings (3).
4. **Lint and nitpick warnings as notices** (13), together with the planned lint in `review`.
5. **The notices**, roughly in order of frequency: PortGroup fit, compiler standard, patch hygiene, dependents ABI.
