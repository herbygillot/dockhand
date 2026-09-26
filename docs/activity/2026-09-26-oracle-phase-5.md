# 2026-09-26: the oracle's phase 5, modelled tools from the facts table

Phase 5 of the [oracle](../oracle.md): a modelled context answers Base's
toolchain questions from the [facts table](2026-09-26-facts-table.md),
not from the Mac dockhand runs on. Before, every modelled release got
this Mac's Xcode, tools, clang, and SDK. The Mac's own context still has
its own tools, which are the truth for it. The two-outcome `java_home`
answer (decision 18), which the scope put in this phase, is phase 5b,
below.

## What changed

- **Which tools a modelled context has.** `macports.Toolchain` reads the
  table's row for the release and architecture in the **Command Line
  Tools profile**. That is dockhand's base images', where its builds run
  by default, and what `ModelVariables` already modelled on hosts that
  aren't Macs. The 09-23 harvest also showed Base compiles with the
  tools' clang and SDK even where Xcode is installed. Where a release
  and architecture have only a buildbot's row, which is Xcode's:
  - its tools are modelled without Xcode, the builder's tools package
    with `xcodeversion` none;
  - what the buildbot doesn't tell, clang's build number and the SDKs,
    comes from an image with the same tools package. The harvest found
    one for darwin 22 and 25 x86_64, and 25 x86_64 is the context
    preparation models most;
  - a release whose builders carry no tools (10.6–10.8, 10.15) is
    modelled as they are, with Xcode;
  - a release the table has no row for, Darwin 8 and 9, keeps the host's
    tools as before, and the ledger says so.
- **Base's parent variables**, set with `override_vars` for the context
  (`macports.ModelVariables`):
  - `developer_dir`, `xcodeversion`, and `xcodecltversion`;
  - `macosx_sdk_version`, which `mportinit` sets from the host's macOS.
    Every modelled context had asked for SDK 26 on this Mac, whatever
    release it modelled;
  - the compiler cache, holding the tools' clang at `/usr/bin/clang` and
    in the tools' directory;
  - `get_tool_path`'s cache, holding `/usr/bin`'s shims.

  Both caches are replaced even where the table is silent, so neither
  the host's nor another context's answers leak in.
- **The worker's questions about the tools' files**, answered by the
  dispatcher's third source, `table` (`toolchain.tcl`):
  - the tools' directory and its programs (the tools' `make`, asked in
    about 22,000 ports);
  - their SDKs by name, including `find_close_sdk`'s glob for a close
    SDK;
  - `/usr/lib/libxcselect.dylib`, which is a file from 10.9 until
    macOS 11 moved system libraries into the dyld cache;
  - `/usr/bin`'s compiler shims, from 10.9 on;
  - Xcode, absent in the tools profile.

  Base's SDK choice now resolves from the table before it ever reaches
  the `xcrun` it would otherwise run on the host.
- **`host-in-model`.** In a modelled context, a question the host still
  answers is counted under that source rather than `host`, as decision 1
  has it. So the ledger shows what the model doesn't cover yet.
- **One mechanism, not two.** A host that isn't a Mac models its own
  context through the same table, with `model_platform` given the
  toolchain answers, and the stand-in `file` in `model_worker` is gone.
  `macos.CurrentToolchain` (Xcode 26.3, clang 1700.6.4.2), which matched
  none of the Tahoe images, is gone too, as decision 13 asked.

## Tests

- `TestAModelledContextTakesItsToolsFromTheTable` evaluates Monterey
  arm64 and Tahoe x86_64 on this Mac. Each has its release's clang
  (1400.0.29.202 and 2100.1.1.101), the tools' developer directory, its
  own SDK (`MacOSX12.sdk`, `MacOSX26.sdk`), `/usr/bin/clang`, the tools'
  `make`, no libxcselect, and no Xcode. The ledger counts the answers
  under `table`.
- `TestCompilerDependentFetchInputComesFromTheTable` replaces the test
  that a compiler-dependent distfile reads the host. In Darwin 16 x86_64
  it is now `host-clang++.tar.gz` with no host read, and Darwin 9, with
  no row, still reads the host.
- `TestPortsThatAskBaseForCompilersReadTheTable`: the eleven real ports
  the 09-24 baseline found reading the host through Base's compiler
  queries, in Darwin 22 x86_64, now read the table.
- The table-derived model's own tests: the image's row, a buildbot's made
  tools-only with clang and SDKs from the matching image, a release with
  Xcode only, and one with no row.

## The survey

The whole tree at `abd9fff84df`, compared with phase 4's survey
(`~/.dockhand/surveys/2026-09-26-phase5-abd9fff.jsonl`, with its ledger):

- **Six ports became conclusive, and none regressed:** OpenBLAS,
  OpenBLAS-devel, and guile-2.2, through the `xcode_workaround` PortGroup,
  and three py-poppler-qt5 subports, whose `use_xcode yes` made Base ask
  for Xcode's SDKs. Their modelled contexts had read this
  Mac's Xcode and tools.
- **Which contexts a fetch is refused in changed for about 190 ports**,
  with no outcome changing. These are Portfile guards that compare
  `xcodeversion`:
  - php below 7.0 refuses Xcode 12 or later. With this Mac's Xcode 26 in
    every context, it had refused even on 10.6–10.8, whose Xcode is
    3.2–5.1. It no longer does there, nor in Tahoe x86_64, which the tools
    profile models without Xcode.
  - gcc8 refuses Xcode before 11.3 on Darwin 19 and later, so in the tools
    profile it now refuses on Darwin 20–24.
- **Cost:** 29,325 CPU seconds and 63.0 minutes, against phase 4's
  32,774 and 69.0.

Outcomes don't show a plan that changed without changing an outcome. So
all 20,119 Portfiles were evaluated with phase 4's code and with phase
5's, in four modelled contexts: Tahoe x86_64, Monterey arm64, 10.14
x86_64, and 10.8 x86_64, which has no tools package. The fetch fields
were compared (`~/.dockhand/surveys/2026-09-26-phase5-fetch-fields/`,
with the scratch test that dumped them and the script that compared
them):

- **Fetch fields changed in 58 contexts of 19 Portfiles**, each where the
  Portfile chooses by the toolchain and phase 4 gave every release this
  Mac's:
  - qt6-qtcreator picks its version by the compiler. It is 15.0.1 on
    10.8 and 10.14 and 18.0.2 on Monterey, not the 19.0.2 every release
    had been given;
  - cctools, on 10.8 and 10.14, fetches ld64 beside cctools, with its
    patches;
  - 17 more choose patch files by `xcodeversion`, the SDK, or clang: gcc5
    and gcc6, llvm 3.7 and 5.0–9.0, qt55's QtWebEngine, mpv, Aerial,
    BGHUDAppKit, Chmox, upc, bmon, jasper, and opam.
- **No evaluation newly failed or succeeded.**
- **Dependencies changed in 721 contexts**, nearly all compilers: a
  `clang-17` build dependency added or dropped by the release's own clang,
  and the old gcc and clang ports' `cctools` and `ld64` where their
  releases need them.

## Which profile

A modelled context is the tools profile, as settled above. Whether that
is right for MacPorts' builders, which all have Xcode, is the one
question this phase leaves. It was measured with a scratch build that
models the Xcode profile, over the same Portfiles and contexts:

- **15 Portfiles fetch different patch files** by profile, mostly by
  comparing `xcodeversion` with a version, which `none` never passes.
- **None of the 15 downloads a patch.** Every patch is a local file under
  `files/`, which carries no checksum, so the profile changes nothing a
  checksum bump edits at this commit.
- Guards like php's and gcc8's still differ by profile, in which contexts
  a fetch is refused.

So the tools profile stays, and the question is open, not decided. If
it is decided the other way, or preparation should cover both where a
Portfile distinguishes them (decision 1), the table already has both
profiles.
