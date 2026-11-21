# Evaluation starts from dockhand's own MacPorts configuration and HOME

An improvement for v0.10.0, [#21](https://github.com/herbygillot/dockhand/issues/21), the final piece of the oracle's bootstrap phase ([oracle.md](../oracle.md), phase 6). The issue says where it stands; this file is its design. Adopted 2026-11-20, after its one review pass; not yet built.

## What exists today, and what an experiment established

- **Evaluation inherits the person's MacPorts config and environment:** `eval.Evaluator` launches `port-tclsh` with the full host environment and calls `mportinit` with no configuration override and the person's real `HOME`. The 2026-11-01 whole-tree survey recorded `mportinit` reading `${prefix}/etc/macports/macports.conf`, `~/.macports/macports.conf`, `sources.conf`, `variants.conf`, `pubkeys.conf`, and environment variables `HOME`, `PATH`, `SUDO_USER`, `SSH_AUTH_SOCK`, `PORTSRC`, `TMP`, `TMPDIR`, and `NO_PROXY`.
- **The closed model (`#110`) does not cover startup:** `#110` closes host reads inside each port's worker interpreter after `mportinit` has already populated `::macports::*` from the host's `macports.conf`, `variants.conf`, and `portdbpath`.
- **The index build isolates only partially:** `portindex` writes a temporary `macports.conf` (`sources_conf`, `variants_conf`, `portdbpath`) passed via `PORTSRC` and sets `LC_ALL=C`, but still passes the rest of the host environment (`HOME`, `PATH`, `SUDO_USER`, `SSH_AUTH_SOCK`, etc.) and lets installation `macports.conf` keys bleed through.
- **What an experiment of 2026-11-20 established (Base 2.12.7 in two prefixes and master 2.12.99; 2.11.6 was not available and is not observed):**
  1. **Restating bare keys in `PORTSRC` does not work.** `PORTSRC` is an overlay, and a bare key leaves the option empty rather than restoring Base's default—breaking `configure.compiler` (via `default_compilers`), `macosx_deployment_target`, `cxx_stdlib`, and `build_arch` on all three Bases.
  2. **Replacing Base's configuration and user directories before `mportinit` works cleanly.** Setting `::macports::autoconf::macports_conf_path` to dockhand's bootstrap directory and `::macports::autoconf::macports_user_dir` to dockhand's empty user directory between `package require macports` and `mportinit` prevented Base from opening either `${prefix}/etc/macports/macports.conf` or `~/.macports/macports.conf` on all three Bases.
  3. **Base's own `macports.conf.default` supplies the stock settings.** Starting from a minimal path-only `macports.conf` drifted on `frameworks_dir`, `applications_dir`, and `startupitem_install`, whereas every tested Base ships `macports.conf.default` with its own stock location keys (8–9 keys). Copying that file and replacing only the five location lines preserves Base's exact defaults without hardcoding any default in dockhand.
  4. **`LC_ALL=C` is safe on non-ASCII Portfiles.** Tcl's encoding stays UTF-8 either way; non-ASCII descriptions evaluated byte-for-byte identically.
  5. **An empty `variants_conf` in `PORTSRC` suppresses `variants.conf` for local checks.** Verified to suppress `+universal` from the prefix's `variants.conf`.

## In short

Every MacPorts evaluation (`eval.Evaluator`—metadata, observation, resolution, `port lint`, `vercmp`, `Inspect`) and every `portindex` build starts under a **dockhand-owned bootstrap directory** containing a copy of that Base's `macports.conf.default` with five location lines replaced, a clean `HOME`, a fresh `portdbpath`, empty `variants.conf` / `archive_sites.conf` / `pubkeys.conf`, a `sources.conf` naming only the evaluated tree, and a minimal environment allowlist.

Local checks (`--on local`) pass an empty `variants_conf` beside their `sources_conf` overlay so local builds match evaluation's default variants unless `--variants` is given (decided by the person, 2026-11-20). No user-facing opt-back-in flag is added for normal dockhand commands; only a survey/test seam exists for the proof survey.

## Rules

### 1. Which of the person's settings are kept, and which are dropped

| Input | Disposition | Rule & Why |
|---|---|---|
| **Ports tree location** (dockhand's materialized tree) | **Kept (dockhand's snapshot)** | Written as the sole `file://<root> [default,nosync]` entry in dockhand's `sources.conf` (empty file for tree-less `Inspect`/`Versions`), and bound via `::macports::sources*` for overlays. The person's `sources.conf` is never read. |
| **MacPorts installation (`port-tclsh` / `portindex` & compiled-in `${prefix}`)** | **Kept** | Located through the person's `--tool port-tclsh`, `PORT_TCLSH_BIN`, `[tools].port_tclsh`, or `PATH` (else `/opt/local/bin`) before launch. Base requires its own `${prefix}/share/macports`, and `installation.tcl` derives the fresh skeleton from `${::macports::prefix}`. |
| **Base's own stock `macports.conf.default`** | **Kept (from the chosen Base)** | Copied from the installation's `macports.conf.default` (which sets `prefix`, `applications_dir`, `frameworks_dir`, etc.) so dockhand never hardcodes Base defaults. |
| **Host OS & native toolchain (native contexts only)** | **Kept (via OS/Base defaults)** | Native contexts still describe this Mac's Darwin version, hardware arch, and installed Xcode/CLT; modelled contexts replace them with `ModelVariables` and the facts table as today. |
| **`${prefix}/etc/macports/macports.conf`** | **Dropped** | Never opened: `::macports::autoconf::macports_conf_path` points at dockhand's bootstrap directory before `mportinit`. |
| **`~/.macports/macports.conf`, `~/.macports/variants.conf`** | **Dropped** | Never opened: both `::macports::autoconf::macports_user_dir` and `HOME` point at dockhand's empty directory (mode `0700`; `HOME` is never unset because `mportinit`'s `dscl` fallback is misquoted on macOS). |
| **`${prefix}/etc/macports/variants.conf`, `sources.conf`, `archive_sites.conf`, `pubkeys.conf`** | **Dropped** | Replaced by dockhand's five location lines pointing into the bootstrap directory (empty files except `sources.conf`). |
| **Host `${prefix}/var/macports` registry & cache** | **Dropped** | Overridden via `portdbpath` pointing at a fresh per-session directory (with `registry/` created before launch) removed on session close, preventing stale `${portdbpath}/cache` facts (`xcodeinfo`, `compiler_versions`, `macos_version`, `pingtimes`) from crossing sessions. |
| **Process environment (`Env`)** | **Minimal allowlist only** | Passed to `port-tclsh` / `portindex`: `HOME=<dir>/home`, `TMPDIR=<dir>/tmp`, `PATH=/usr/bin:/bin:/usr/sbin:/sbin`, `LC_ALL=C` (plus `PORTSRC` only if the `portindex` fallback in §3 is needed). Ambient `PORTSRC`, `SUDO_USER`, `SSH_AUTH_SOCK`, `JAVA_HOME`, `DEVELOPER_DIR`, `CCACHE_DIR`, `DYLD_*`, `DISPLAY`, `TMP`, `*_SITE_LOCAL`, and proxy variables are **dropped**. |

**Visible (and intended) behavior change from minimal `PATH`:** Dockhand still finds `port-tclsh` and `portindex` on the person's `PATH` before launch, and `mportinit` prepends `${prefix}/bin:${prefix}/sbin`, but **native contexts will no longer find programs under `/usr/local/bin` or Homebrew (`/opt/homebrew/bin`)** during evaluation. This is wanted—evaluation matches a clean MacPorts prefix rather than incidental host tools.

### 2. Bootstrap mechanism and fail-closed verification

For each evaluator session (`eval.Evaluator`), dockhand creates a temporary bootstrap directory under `internal/scratch` (removed on `Session.Close` and on startup failure):

1. Locate `macports.conf.default` beside the chosen Base's `macports.conf` (from `port-tclsh`'s prefix, `<prefix>/etc/macports/macports.conf.default`). If it is missing or unreadable, or sets no `prefix` (Base refuses to start without one), fail closed naming the path.
2. Write `<dir>/macports.conf` as a copy of `macports.conf.default` with **only these five location keys replaced** (appended if absent, replacing any existing setting for those keys, preserving `prefix` and every other stock line verbatim):
   - `portdbpath <dir>/portdb`
   - `sources_conf <dir>/sources.conf`
   - `variants_conf <dir>/variants.conf`
   - `archive_sites_conf <dir>/archive_sites.conf`
   - `pubkeys_conf <dir>/pubkeys.conf`
3. Create `<dir>/portdb/registry`, `<dir>/home`, `<dir>/tmp`, empty `variants.conf`, `archive_sites.conf`, `pubkeys.conf`, and `sources.conf` (`file://<root> [default,nosync]` when a tree is given, empty otherwise).
4. In `evaluator.tcl`, after `package require macports` and before `mportinit`:
   - Check that `::macports::autoconf::macports_conf_path` and `::macports::autoconf::macports_user_dir` exist. If either is absent on a Base release, **fail closed** with a startup error naming the missing variable.
   - Set `::macports::autoconf::macports_conf_path` to `<dir>` and `::macports::autoconf::macports_user_dir` to `<dir>/home`.
5. **Post-`mportinit` origin guard (fail closed):** Always verify (using the origin check `bootstrap_settle` already has for surveys) that no `mportinit` configuration file opened during startup lies outside `<dir>`. If any setting traces to a file outside dockhand's bootstrap directory (for instance, if a future Base adds another config path), refuse evaluation with an error naming that file.
6. Document `::macports::autoconf::macports_conf_path` and `::macports::autoconf::macports_user_dir` in `docs/macports-compatibility.md`'s list of Base internals.

Because `eval.Session` already reuses one interpreter across repeated probes of a Portfile, the ~27 ms cost of a fresh `portdbpath` is paid once per session rather than per probe.

### 3. One shared bootstrap for `eval` and `portindex`

One shared bootstrap helper builds the directory, copies and edits `macports.conf.default`, produces the minimal `Env`, and cleans up on `Close()`.

- **`portindex`:** `portindex` is a Tcl script on all tested Bases, run by the interpreter its first line names, which the index build already finds for its runtime probe. The primary path launches that interpreter with a small wrapper that loads `macports`, sets `::macports::autoconf::macports_conf_path` and `::macports::autoconf::macports_user_dir`, and then sources `portindex` with the arguments the index build passes today (its `-p file:<index_vars>` platform file, `-o`, and the tree), unchanged. No `PORTSRC` is set on this path, and an ambient one is dropped.
- **First test to write:** Confirm that this wrapper works cleanly for `portindex` on Base 2.11, 2.12, and master (not yet tried). If a Base's `portindex` script does not tolerate pre-loading `macports`, `portindex` falls back to passing `PORTSRC=<dir>/macports.conf` (containing `macports.conf.default`'s stock values + the five location overrides, which the experiment confirmed restores keys that have values) with the same clean `HOME` and minimal `Env`. That fallback cannot undo a key the installation's `macports.conf` sets and the stock file leaves to Base, such as `default_compilers`, so under it the index build compares the two files first and, when the installation's sets one the stock file doesn't, fails with a message naming the key and the file. No index is ever built outside dockhand's bootstrap.

### 4. Local provider ignores the prefix's `variants.conf` (decided 2026-11-20)

Without this, a person with a `variants.conf` would get plans and "Tested on" evidence claiming default variants while `--on local` silently built with their `variants.conf` applied.

- `internal/buildenv/local` writes an empty `variants.conf` in the check's staging directory and adds `variants_conf <dir>/variants.conf` beside the `sources_conf` line it already writes in its `PORTSRC` file (observed to suppress `+universal`, 2026-11-20).
- `--variants` remains the explicit way to ask a local check for other variants.
- Everything else in the local prefix—its `macports.conf`, its registry, and its installed ports—stays as it is, because `--on local` builds in the person's MacPorts prefix for real.

### 5. No user-facing opt-back-in

No CLI flag or `config.toml` key lets normal dockhand commands evaluate against the person's `macports.conf` or `variants.conf`.

- Dockhand prepares commits and pull requests for `macports/macports-ports`, which MacPorts CI and buildbots evaluate with stock `macports.conf` and empty `variants.conf`.
- Explicit variant testing already works via `dockhand check --variants`, and selecting another MacPorts Base installation already works via `--tool port-tclsh` / `PORT_TCLSH_BIN`.
- **Survey/test seam only:** `eval.Evaluator` gains an internal `HostConfig bool` field (parallel to `Installation` and `Adapter`, set by no CLI command) wired to a `-host-config` flag in `tools/survey` solely for the before-and-after proof survey and isolation tests.

### 6. Invalidating stored results made under host settings

Results recorded before `#21` may have been produced under the person's `variants.conf` or `macports.conf` and must not be reused afterwards. Three existing cache/policy keys bump in the same change:

1. **Port index cache (`internal/macports/portindex`):** Bump `cacheLayout` from `portindex-generations-1` to `portindex-generations-2` (part of the indexing environment digest and `environment.json`), so pre-`#21` index generations are never reused.
2. **Stored change records (`internal/macports/fidelity`):** Bump `ChangePolicy` (from `9` to `10`), so change records evaluated under host settings are recomputed cleanly.
3. **Stored assessments (`internal/macports/assess`):** Bump `Policy` (from `30` to `31`), so assessments derived from host-configured evaluations are not reused.

## Not taken

- **Hardcoding Base's default `macports.conf` values in dockhand:** Creates a second copy of Base defaults that drifts across Base releases and fails for unset-by-default keys like `default_compilers` (observed 2026-11-20).
- **Bare keys in `PORTSRC`:** Observed to leave options empty rather than restoring Base defaults.
- **Leaving `--on local` with the prefix's `variants.conf`:** Silently skews local build verdicts relative to evaluation and "Tested on" (settled by the person, 2026-11-20).
- **User-facing opt-back-in flag or config key:** No consumer and risks skewed upstream PRs; survey seam suffices.
- **Unsetting `HOME`:** Breaks `mportinit`'s misquoted `dscl` fallback on macOS.
- **Sharing one persistent `portdbpath` across processes:** Leaks cached `xcodeinfo` / `compiler_versions` between sessions.

## Tests & Proof

### Automated tests
- **`portindex` wrapper (first test to write):** Verify `portindex` runs cleanly under `::macports::autoconf::macports_conf_path` / `macports_user_dir` and ignores polluted `HOME`, `PORTSRC`, `variants.conf`, and `PATH` shims.
- **Evaluator isolation & fail-closed checks:**
  - Polluted `Env` (`HOME` with `~/.macports/macports.conf`, ambient `PORTSRC`, `SUDO_USER`, `SSH_AUTH_SOCK`, `JAVA_HOME`, `DEVELOPER_DIR`, `NO_PROXY`, `PATH` with fake `/usr/local/bin` tools) leaves `bootstrap_record` reading only dockhand's bootstrap directory and allowed env keys.
  - Non-default `variants.conf` (`+universal`) and polluted installation/user `macports.conf` (`macosx_deployment_target`, `build_arch`, `cxx_stdlib`, `default_compilers`, `applications_dir`) do not alter evaluated `PortInfo`, `configure.compiler`, or `bootstrap_settings`.
  - Missing `macports.conf.default`, missing `::macports::autoconf::*` variable, or a startup read escaping the bootstrap directory fails closed with a clear message naming the missing variable or file.
  - Lifecycle cleanup: `Session.Close`, standalone `Evaluate` / `Lint` / `Inspect` / `Versions`, and failed startups leave no bootstrap directory behind.
- **Local provider:** Verify a local check suppresses `+universal` in the prefix's `variants.conf` while still honoring explicit `--variants`.
- **Cache invalidation:** Verify pre-`#21` `portindex-generations-1` entries, old `fidelity.ChangePolicy` records, and old `assess.Policy` records are not reused.
- **Base matrix:** Run `go test ./internal/macports/...` against Base **2.11.6** (not observed in the experiment), **2.12.x**, and **master** (`DOCKHAND_TEST_BASE_ADAPTER=preview`, confirming port opening works cleanly inside dockhand's real evaluator suite).

### Proof survey (required by `#21`)
Run a sampled before-and-after survey with `tools/survey` using identical flags:
1. **Default Mac:** Compare `-host-config=true` vs. `-host-config=false` — confirm zero evaluation results move.
2. **Non-default configuration (a prefix or `~/.macports` setting `+universal` in `variants.conf` and non-default `macports.conf` keys):** Confirm `-host-config=false` matches the default Mac's results identically, while `-host-config=true` drifts.

## Usage docs

Updated in the same commit:
- `docs/oracle.md` (mark Phase 6 done and record the bootstrap contract).
- `docs/macports-compatibility.md` (update "Isolating evaluation" to current behavior, add `::macports::autoconf::macports_conf_path` and `::macports::autoconf::macports_user_dir` to Base internals, and note that native evaluation no longer searches `/usr/local` or Homebrew on `PATH`).
- `docs/local-provider.md` (document that `--on local` ignores the prefix's `variants.conf` and takes other variants via `--variants`, while keeping the prefix's `macports.conf` and installed ports).
- `docs/decisions.md` (recorded at adoption; the entry is marked built).

## Order

Independent of v0.9.0's core (`#191`–`#195`); ready to build in v0.10.0. Base 2.11.6 and the `portindex` wrapper are verified as it is built.
