# MacPorts evaluator compatibility

Dockhand evaluates Portfiles with released MacPorts Base 2.11 and 2.12, and checks the capabilities it uses on those as well. The version is read once the `macports` package is loaded, before `mportinit` reads the host's configuration. An older release is refused with a pointer to `sudo port selfupdate`; a release of a newer family, as 2.13.0 will be, is admitted with one line saying dockhand wasn't built against it and may read Portfiles wrong where Base changed, so the day MacPorts ships one doesn't stop every command that reads a Portfile; an unrecognized version is refused. A development build (Base master reports x.y.99) is refused as well: master asks host questions from its parent interpreter, dockhand answers only the ones it knows of there, and master may move more, so preparing with it could rest on answers dockhand never saw ([Base master](#base-master)). The evaluator's own tests can run against one with `DOCKHAND_TEST_BASE_ADAPTER=preview`; no command admits one until the preview adapter exists.

`setup` reports the host Base and Tcl versions, successful startup checks, and whether that exact Base release has a source-review record. Its JSON result includes `host_macports`. Evaluated snapshots retain the observed runtime information. A source-review record does not certify a local installation or its PortGroups.

Startup checks require the native metadata/open/close interfaces and dependency-item access before `mportinit`, and working MacPorts version comparison after it, since master loads `vercmp` only in `mportinit`. Opening a port checks the worker's option access. Missing interfaces stop evaluation with the observed Base version and guidance to check `--prefix`/`port-tclsh` or update Base.

Fetch inspection probes native target/hook registration with an inert temporary target, checks the generated wrapper convention, then reads the selected port's fetch target. The probe never executes a fetch hook, modifies source files, or fetches an archive. It removes its temporary target and commands. Unknown record layouts, wrapper conventions, custom fetch procedures, and unsupported hooks disable automatic archive preparation. Metadata can still be read, and preparation reports the reason and suggests manual preparation. Actual Go PortGroup hook recognition remains structural and separate from Base capability checks.

## Evidence

| Base | Evidence |
| --- | --- |
| 2.12.6 | Evaluator suite and capability-failure tests on installed Base/Tcl 8.6.17, Darwin 25 arm64. |
| 2.11.6 | Same evaluator suite on an isolated build from the local `v2.11.6` tag, Tcl 8.6.16, Darwin 25 arm64. Newer host-based credential-selector tests skip; legacy selector tests pass. |
| 2.12.2–2.12.5, 2.10.7, 2.9.3, 2.8.1 | Earlier source review only; 2.10 and older are refused. |
| 2.12.99 (master) | The evaluator's and `internal/macports` tests pass with `DOCKHAND_TEST_BASE_ADAPTER=preview`, Tcl 9.0.4, Darwin 25 arm64, in a weekly CI job against master and locally; since 2026-10-27 (33251823, 0c1aa135) that includes the parent-side SDK, tool, and compiler-list regressions, unskipped. Refused otherwise. |
| Other versions | No source-review record; refused if older than 2.11, and a newer family's release admitted with a warning. |

These tests cover synthetic Portfiles, source-bound resources, variants, subports, metadata failures, fetch-hook inspection, credentials, and version comparison. They do not certify every current PortGroup or port build on these Base versions.

Run the evaluator suite against another isolated installation with:

```sh
DOCKHAND_TEST_MACPORTS_TCLSH=/path/to/prefix/bin/port-tclsh \
  go test ./internal/macports/eval -count=1 -v

# Base master, which the evaluator admits only with the preview adapter:
DOCKHAND_TEST_BASE_ADAPTER=preview \
DOCKHAND_TEST_MACPORTS_TCLSH=/opt/macports-master/bin/port-tclsh \
  go test ./internal/macports/... -count=1
```

Select an appropriate compiler/SDK when building the older installation. The implementation continues to share one Tcl adapter; a separate version adapter should be introduced only for a demonstrated contract difference.

## Base master

Base master is what the next release will be, so what it already does differently is recorded here, from a baseline against master at `0c70cb739` (2.12.99, Tcl 9.0.4) on 2026-09-24 and the weekly job since.

- `macports::version` is set before `mportinit` on 2.12 and on master alike, but master loads Pextlib, and with it `vercmp`, only inside `mportinit` (e545ebe8c). The startup checks are split around it for that reason.
- Master loads the fetch helpers lazily: `portfetch::checkfiles` and its neighbours exist only after `portutil::target_load` for the fetch target, as Base's own `fetch_async_start` calls it. That costs about 11.5 ms per port interpreter, against under 1 ms on 2.12.6.
- Master runs the SDK, developer tool, and compiler lookups (`get_sdkroot`, `find_close_sdk`, `file_exists`, `find_developer_tool`, `get_system_compiler_version`, `get_clang_compilers`) in its parent interpreter, `portlib::configure`, aliased into each worker, and caches their answers for the whole process (`file_exists_cache`, `sdkroot_cache`, and `clang_compilers_cache`, the last keyed by the tree's list file alone). On 2026-09-24 eleven ports that read the host looked host-independent on master: qt4-mac, qt64-qtwebengine and its docs, poedit, godot, openjdk8, gcc48, gcc49, gcc8, libgcc8, and gpsd. A modelled context's worker now gets its own copies of these, with its own caches; `get_tool_path` and `get_compiler_version` stay Base's.
- Master's `portlib` also caches mirror URLs by mirror file, not by platform: a URL built from `${os.major}` kept Darwin 22's value in a Darwin 25 context of the same process until that cache was cleared (2026-09-24). The worker's lookup copies don't cover this cache.
- `quotemeta` and `getdisttag` are parent aliases on master, so a fetch hook that calls one is guarded on 2.12.6 and refused on master. No `pre-fetch` hook in the tree called either directly (2026-09-24).
- Master derives extract dependencies from `extract.only`'s suffixes (14fdd90f9, 5c05e1eff): about 20 ports gain or lose `bin:xz:xz` or `bin:unzip:unzip`. An expected output that includes dependencies is per Base version for that reason.
- Livecheck's default-type logic, which the evaluator copies to run livecheck offline, moved to `portlivecheck_run.tcl`'s `_livecheck_main` on master.
- Tcl 9, which master builds with: `glob` without `-nocomplain` returns an empty list where 8.6 raised an error (Base's own guard at `portlivecheck_run.tcl:101` relies on the error); a non-UTF-8 Portfile is refused, and none exist; `file normalize` no longer expands `~`; `expr 010` is 10; and `encoding convertfrom` of invalid UTF-8 raises, which ends a session where it isn't caught.
- 659 Portfiles (2,570 ports and subports, 9,499 distfiles: every Portfile with a `pre-fetch` hook, PortGroup samples, and 250 at random) gave the same fetch plans, fetch-guard verdicts, option errors, and livecheck values on 2.12.6 and on master (2026-09-24).
- `port-tclsh` with `package require macports` and `mportinit`, warm, took 34.5 ms on 2.12.6 and 37.7 ms on master (2026-09-24). Evaluating one port costs 20 to 30 ms untraced (2026-09-20).

## What dockhand reaches inside Base

Base's lead developer drives Base through `port-tclsh` in his own scripts, as mpbb, the build scripts MacPorts' CI and buildbots run, does. Dockhand does the same. `package require macports`, `mportinit`, `mportopen` with subport and variants, `mportinfo`, `mportclose`, the port's interpreter through `ditem_key $h workername`, `macports::os_*`, `build_arch`, and `macports::version` are the application interface Base's `doc/INTERNALS` describes, and were stable from 2.8 to master. `macports::override_vars` (2.11.0 and later) models another platform, as mpbb's `mirror-multi.tcl` and `portindex -p` use it, and removes variable traces as it does.

Beyond those, dockhand alone relies on these, each guarded so that a change disables the operation that depends on it rather than letting it evaluate wrongly:

- a `leave` trace on `::macports::worker_init`, and one on `PortSystem`, to run code in each port interpreter;
- declaration and host-access traces read through `info frame`;
- target and hook records through `ditem_key`, Base's `user<proc-…>` hook wrapper and body prefix, and the fetch target's procedure compared with `portfetch::fetch_main`;
- `portfetch::checkfiles`, `urlmap`, and `assemble_url`, with `ports_fetch_no-mirrors`, for a fetch plan without fetching;
- `::macports::fetch_credentials` and a copy of `curlwrap`'s credential logic, which change often; the probe adapts and fails closed;
- the copy of livecheck's default-type logic;
- `::macports::sources*` and `porturl_prefix_map` (2.10.0 and later), written after `mportinit` to make a branch's tree the source.

## Isolating evaluation

Evaluation still starts from the host's configuration. The oracle's bootstrap phase ([oracle.md](oracle.md), phase 6) and the roadmap's environment contract would start it from dockhand's own, and rest on these observations of 2.12.6 and master (2026-09-24):

- `PORTSRC` is read after the installation's and the user's `macports.conf`: an overlay, so a key it doesn't restate keeps their value.
- `HOME` set to an empty directory hides `~/.macports/macports.conf`. Unsetting `HOME` breaks `mportinit`, whose fallback through `dscl` is misquoted.
- A private `portdbpath` gives an empty registry, and moves `distpath`, `ccache_dir`, and the image mode with it; its `registry/` directory has to exist first. Base keeps host facts under `${portdbpath}/cache` (`xcodeinfo`, `macos_version`, `compiler_versions`, `pingtimes`), so a directory kept between processes carries them: a second master process returned a compiler version the first had saved, for a compiler that doesn't exist. A fresh directory costs about 27 ms per `mportinit`.
- Left to the host unless restated or modelled: `archive_sites_conf`, `pubkeys_conf`, `archivefetch_pubkeys`, `startupitem_install`, and `macportsuser`; the install user and group and tool paths compiled into Base; the environment `mportinit` keeps (`HOME`, `JAVA_HOME`, `PATH`, `*_SITE_LOCAL`, proxies, `DYLD_*`, `DISPLAY`) and reads (`SUDO_USER`, `SSH_AUTH_SOCK`, `CCACHE_DIR`); and Xcode and Command Line Tools detection.
- Base installed in one prefix can evaluate as another: a `PORTSRC` setting `prefix /opt/local` and the paths that follow it made a Base installed elsewhere evaluate `${prefix}` as `/opt/local` and write nothing there. `mportinit` still requires `${prefix}/share/macports` to exist, puts `${prefix}/bin` on `PATH`, and prefix-relative probes read the real prefix. Tool paths are compiled in from the build's `PATH`, so such a Base has to be built with a clean one.

## Observed in the tree

Facts about the ports tree and MacPorts that dockhand's code relies on or works around, each with the date it was seen. The counts are of the tree on that date.

### PortGroups

- **golang** sets `worksrcdir` to `gopath/src/<package>`, which no archive holds, and its post-extract moves the archive's top directory there; with `extract.rename 1` it renames that directory to the whole `worksrcdir` (lima 2026-10-03, maddy 2026-10-15).
- **golang** makes each `go.vendors` row a distfile from the module's own host, `codeload.github.com/<author>/<project>/legacy.tar.gz/<lock>` for GitHub, never proxy.golang.org's zip, so the proxy's checksums don't match what MacPorts fetches (2026-10-15). It appends each row's checksums in separate commands, the file alone first (`golang-1.0.tcl:521`), and where `go.vendors` lists a module it turns `extract.rename` off and wraps `extract.cmd` (git-credential-oauth, 2026-10-23).
- **golang**'s `go.setup` fetches from six hosts, github.com, gitlab.com, bitbucket.org, git.sr.ht, codeberg.org, and gitea.com, and registers its toolchain check whatever `go.domain` is (reposurgeon on gitlab.com, 2026-10-02). `golang.org/x/<repo>` is fetched from its GitHub mirror, github.com/golang/<repo> (2026-10-27).
- **golang**'s ports share `dist_subdir go`, and MacPorts tries its distfiles mirror first. gopls 0.24.0's GitHub tag archive failed its checksum because the mirror already held x/tools' own `tools-0.24.0.tar.gz` under `go/`; the fix was `dist_subdir ${name}` (#35546, 2026-10-15).
- **cargo**'s `cargo.crates_github` row carries a branch, which post-extract writes into Cargo's source replacement as `branch = "…"`. Cargo matches a replacement by URL and selector kind, so a source the lock pins with `rev=` can't be replaced, and maintainers empty `cargo.offline_cmd` instead, as codex and mcfly do (2026-09-16); pgdog declares its `rev=` crates anyway, labelled `master` (2026-10-02).
- **rust 1.0**'s `checksums-append` (`rust-1.0.tcl:253`) carries each `cargo.crates` row's sha256, which the Portfile writes (2026-10-20).
- **crossbinutils 1.0 and crossgcc 1.0** set the checksums of 73 cross ports from their own tables, `crossbinutils.versions_info` and `crossgcc.versions_info`, which no Portfile edit changes (2026-10-19).
- **qt5 and qt6** keep every module's checksums and revision in one `array set modules {…}` table, read with nested `lindex` inside a `foreach`, for about 517 subports; pcre keeps an array per subport (2026-09-17). On Darwin 18 to 22, qt6 loads the obsolete PortGroup and names `qt64-` or `qt67-` successors, so those contexts fetch nothing (2026-09-17).
- **compilers** is loaded by every R port, and its pre-fetch guard calls `fortran_variant_name` (2026-09-17).
- **python** subports have no block of their own: they take the Portfile's revision, and the PortGroup turns their livecheck off (py-lmdb, py310-cbor2, 2026-10-02). Its default was 314 on 2026-09-28 (`python_get_default_version`). 2,256 `py-*` stubs fetch nothing of their own, and 2,324 of the 2,440 python update commits of the year before 2026-09-17 are titled by the stub, `py-foo: update to X`.
- **perl5**: `perl5.setup JSON 4.11` evaluates to version `4.110.0`, while CPAN and `livecheck.version` say `4.11`, and CPAN's livecheck is `regexm`, one match against the whole listing (2026-09-17). 32 modules spell versions `v2.403.9`; `vercmp` orders them and the PortGroup strips the `v`. A few livechecks name a CPAN author directory without its trailing slash, which CPAN redirects to plain http; dockhand refuses that redirect, so those ports need an explicit version (2026-09-18).
- **qmake5 1.0** reads the host as it loads: its `platform macosx` block reads `use_xcode`, whose default Base computes from whether `/usr/lib/libxcselect.dylib` exists, and checks for `${configure.developer_dir}/SDKs` (librecad, cpi, 2026-10-20).
- **xcodeversion** is how 166 Portfiles state a minimum Xcode (2026-09-27), as sand-runner's `minimum_xcodeversions {24 26.0}` does (2026-10-01).
- **`os.arch`** is tested by 116 Portfiles and 11 PortGroups, 20 of them `eq "arm"` and 6 `ne "arm"`; the compilers and common_lisp PortGroups gate on it, and games/lbreakouthd adds autoconf, automake, and libtool on arm (2026-09-22).

### go2port and cargo2port

- go2port writes `go.vendors` from `go.mod`'s requirements. It leaves out test-only modules, so macpine keeps c2sp.org/CCTV/age and gopkg.in/check.v1 by hand (2026-10-03), and before Go 1.17 a `go.mod` needn't list every module the build reads, so its output for countdown 1.5.0 (`go 1.14`) lacked rivo/uniseg and couldn't build (2026-10-06).
- go2port locks a module in a repository's subdirectory by the repository's tag, `term/v0.2.1` where `go.mod` requires v0.2.1 (walk 1.13.0, 2026-10-07). It reads a module at its repository's root, so a module below the root whose `go.mod` replaces its parent with `../`, as gopls's does, gets no usable `go.vendors` (2026-10-27).
- go2port fetches each module from its host itself. For a repository that's gone, github.com/chigopher/pathlib, it writes a sha256 of 0 (2026-10-23). An older go2port wrote rows without the major-version suffix, go-textseg for go-textseg/v13 (tfvar, 2026-10-18).
- cargo2port ignores Git sources (2026-09-16), and lays out a long crate version from the column's left edge, running past it and pushing the checksum along (2026-09-23).

### Fetching, checksums, and livecheck

- 1,288 Portfiles had no sha256 or an ambiguous group: 557 `md5 sha1 rmd160`, 323 `md5` alone, 69 `rmd160 sha1`, 48 `md5 sha1`, 41 `sha1` alone, and 6 `rmd160` alone, with about 240 more carrying sha256 beside `md5` or `sha1` (2026-09-17). About 110 ports fetch only from FTP (2026-09-18).
- 147 ports use `fetch.type git`. 109 derive `git.branch` from the version through `github.setup`, `go.setup`, or `gitlab.setup`; 31 pin a literal 40-hex commit, so they have no tag to discover and update needs an explicit version; 19 also checksum an archive (2026-09-17).
- 19 ports use `livecheck.type git`. Base's git livecheck reads no tags: it runs `git ls-remote <livecheck.url> <livecheck.branch>`, `HEAD` by default, and compares the commit with `livecheck.version` (2026-10-02).
- A default livecheck resolves through the tree's `_resources/port1.0/livecheck/` definitions, and `master-sites.tcl` there reads `livecheck.distname` (2026-10-02).
- MacPorts lists twenty SourceForge mirrors. SourceForge answers a request that carries a `Referer`, as Go's HTTP client adds on a redirect, with its HTML download page, from every mirror; curl and MacPorts send none (2026-10-19).
- GitHub lays its JSON out by user agent: only an agent naming both MacPorts and libcurl gets the indented form maintainers' livecheck expressions are written against (2026-09-21). MetaCPAN answered HTTP 402 to Go's default user agent (2026-09-17).

### PortIndex

- `portindex -p <os>_<major>_<arch>` takes `os_arch` verbatim from the third field, where Base sets it to what `uname -p` says, `arm` or `i386`, and the mirror names its index directories by that, `PortIndex_darwin_25_arm`. MacPorts' mirror workflow passes `macosx_<major>_$(uname -p)` and mpbb's `index_vars` file for the platform (2026-09-22).
- A PortIndex record's length counts Tcl's UTF-16 code units, not bytes, so a character outside the Basic Multilingual Plane counts twice (2026-09-14).
- lang/cabal and lang/ghc didn't index in the tree of 2026-09-14, so an incremental index tolerates parse errors outside the directories a branch changes.
- A cold full index took 3 min 12 s to 4 min 24 s on the September hardware, and an update for a few changed directories about 4 s (2026-09-14 to 2026-09-16).

### Lint, CI, and binary archives

- `port lint` refuses `nomaintainer` beside another maintainer, and `openmaintainer` alone (2026-09-28); it refuses a license name ending in a digit, as `CC0-1` (2026-09-30); it reported nothing of trailing spaces (2026-10-20). It fails on errors alone. MacPorts' workflow lints without `--nitpick` and writes the warnings as an annotation, `port lint NAME:%0AWarning: …` (2026-10-27).
- MacPorts' CI runs `port lint`, then `mpbb install-dependencies`, then `mpbb install-port`, whose `port -d fetch`, `port -d checksum`, and install are each a new `port` command, so the target is evaluated with its dependencies active. It builds default variants, every subport of each changed directory, without trace mode, and runs `mpbb test-port` without failing the build (2026-09-23, 2026-09-24). Its `bootstrap.sh` pins Xcode 16.2 on Darwin 23 and 26.4 on Darwin 25 (at `abd9fff84df`, 2026-09-27).
- Within one `port` command, `mportopen` evaluates the Portfile and its callbacks, `mportdepends` then installs the dependencies, and the phases run in the same interpreter without evaluating again: `distfiles`, `checksums`, `depends_*`, and the compiler are fixed before the dependencies exist (2026-09-23).
- `buildfromsource ifneeded`, the default, installs a published archive wherever one matches the port's version, revision, and variants, so `port install` of a port whose Portfile changed in neither doesn't build it (s2n-tls, 2026-09-30).
- `port -N -b archivefetch <port>` fails exactly where no binary archive is published for this release, architecture, and variants (2026-10-16).
- packages.macports.org names an archive as the buildbots make it, `jq-1.8.1_0+docs.darwin_25.arm64.tbz2`, its variants in name order, at `<port>/<name>`. The buildbots build a version some hours after it lands on master (2026-10-27).
- packages.macports.org serves rmd160 and signify signatures both. MacPorts' prefetch creates an empty signature file for each type, then takes the first type whose file exists, even an empty one, in a Tcl array's order (2026-09-28).
- Before fetching, MacPorts 2.12 looks for an archive in `software/<port>/` only for a port it has installed, and otherwise in `incoming/verified`; MacPorts documents neither, only `archive_sites.conf` and `pubkeys.conf` (2026-09-28). In `directory` image mode it never writes an archive (2026-09-27).
- The buildbot's API lists each builder's latest build of a port by time, not by the version built, and every builder runs full Xcode (2026-09-27). Their logs print Base's header but not the compiler's version, the developer directory, or the SDKs (2026-09-26).
- A release's Intel and arm64 builders can run different tools, Command Line Tools 26.0 against 16.4 on macOS 15 and 15.3 against 16.2 on macOS 14, so the facts table's rows are per architecture; x86_64 rows come from the buildbots alone, since Tart boots arm64 macOS only (2026-09-26). Base keeps its own minimum, "ok", and recommended Xcode for each macOS, `macports::get_compatible_xcode_versions` (2026-09-27).

### Base

- A `port:` dependency names its port in the last field, after fields Base reserves and ignores (`^(port)(:.+)?:([-._A-Za-z0-9]+)$`, "colon-separated junk", c0a7e26c7, 2008, trac #126). `restore::resolve_depspec` (2024) alone takes the second field, so it would read mise's `port:bin/cmake:cmake` as `bin/cmake`. mise is the only port that uses the form. It looks like a Base bug, unreported (2026-09-29).
- `port -q dir` with a name it can't resolve fails for the whole list (2026-09-27).
- MacPorts 2.12.6's package leaves seven directories of its install skeleton for a port or MacPorts to make later, such as `lib/pkgconfig` and `var/macports/software` (2026-09-28).
- Trace mode (`port -t`) hides every file under the prefix that no declared dependency installed, and reports each access it denied. It changes the build it watches: an undeclared dependency fails, or the build configures without it (2026-10-22).
- Evaluation is read-only in practice. Traced over the whole tree on 2026-09-23 (41,771 ports), no Portfile or Base code wrote a file or reached the network while evaluating. About 210 ports ran some 20 programs, the Java PortGroup's `java_home` (183 ports) most of them, then `xcrun --show-sdk-path`, `sysctl`, `uname`, `machine`, `git -C`, `rustc`, `clang --print-search-dirs`, and the prefix's `perl` and `ruby`.
- MacPorts' installer package keeps configuration it finds and ends by running `selfupdate`; a `sources.conf` whose one source is marked `nosync`, as MacPorts' own CI writes, keeps that from fetching a ports tree, which took dockhand's CI install from 26 s to 6 s (2026-09-28).
