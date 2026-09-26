# 2026-09-26: the oracle's phase 4, a fresh installation

Phase 4 of the [oracle](../oracle.md): every port is evaluated against a
fresh MacPorts installation, whatever this Mac has installed. That means
Base alone in its prefix and an empty registry, as MacPorts CI and a new
Mac start from. It is decision 2 of the scope, made concrete as the
design's `fresh` installation source. Phase 3 comes later: it needs Tcl to
call Go, and serves the sparse workspace and reuse more than a plan's
correctness.

## What changed

- **An installation source in the dispatcher.** `installation.tcl` gives
  every worker's dispatcher the fresh installation's answers, and they
  never reach the host's prefix or registry:
  - **the registry.** Base's read aliases (`registry_active`,
    `registry_open`, `registry_exists`, `registry_exists_for_name`,
    `registry_file_registered`, `registry_port_registered`,
    `registry_list_depends`, the two `fileinfo` aliases, and
    `_portnameactive`) answer as an empty registry does, in Base's words.
    `registry_active` throws `Registry error: <port> not registered as
    installed & active.`, on which Portfiles and `active_variants` rely.
  - **the prefix.** Under the prefix, a path is there only if it is Base's
    own (`libexec/macports`, `share/macports`, `etc/macports`,
    `var/macports`, and its programs in `bin`) or a directory of the
    skeleton Base's installer makes from its `prefix.mtree` and
    `base.mtree`. Everything in the applications directory is absent.
    `file`, `open`, and `glob` answer for an absent path as Tcl would for
    one that doesn't exist, in Tcl's own words. They do it by asking the
    same question of a path under a directory that doesn't exist, and
    putting the original path back in the answer.
  - **programs.** An `exec` or `open |` of a program the fresh prefix
    lacks fails as exec does for a missing one. A bare name the prefix
    would have shadowed along `PATH` runs from where a fresh installation
    finds it: `perl` from `/usr/bin`, not `${prefix}/bin`. `PATH` itself
    is already MacPorts' build path, which `mportinit` sets.
  - **Base's lookups.** `findBinary` and `binaryInPath` run in the parent
    and would see the host's prefix. The worker gets the same two
    procedures over the fresh installation, and their reads stay as
    unobserved as they were in the parent.
- **The ledger names the subject.** Each entry has a fourth field, what
  a question of the installation was about: the port a registry question
  names, or the program an exec or lookup looked for. So every survey
  keeps the registry's subjects current, and the 2026-09-23 extraction's
  lower bound is no longer needed.
- **A fresh answer isn't a host read.** The installation answers from
  the model, so it raises no host-access event, and a modelled context
  that read the prefix is no longer inconclusive on that account.
- **A latent phase-1 bug, fixed.** `file stat` and `file lstat` set an
  array in their caller's frame. Through the dispatcher they set it in
  the dispatcher's own frame, where it was lost. They now run at their
  caller's level.

## Tests

`TestTheInstallationIsFresh` checks the fresh answers against what this
Mac actually has installed, found when the test runs:

- a port the registry holds is neither active nor registered;
- a program only the prefix has doesn't exist, can't be read, opened,
  or run, by path or by name, and `findBinary` doesn't find it;
- a program the prefix shadows runs from `/usr/bin`. `perl
  -V:installprefix` answers `/usr` rather than the prefix;
- Base's own files and the skeleton's directories are there, and a
  glob of `${prefix}/bin` finds only Base's programs.

Each part is skipped where the machine has nothing to test it with.
`TestTheLedgerNamesWhatTheInstallationWasAsked` finds the registry
questions' subjects in the ledger. `TestFileStatSetsItsCallersArray`
covers the stat fix.

## The survey

The whole tree at `abd9fff84df`, 41,790 ports, compared with phase 2's
survey (`~/.dockhand/surveys/2026-09-26-phase4-abd9fff.jsonl`, with its
ledger beside it). This Mac's test prefix has a good deal installed,
2,570 programs in `bin` alone, so phase 2 saw it and phase 4 doesn't.

- **62 ports moved, all from unknown to input-found, and none back:**
  - **61** had modelled contexts made inconclusive by a read of the
    prefix, which the fresh installation now answers:
    - 30 php subports;
    - 10 ports of the elisp PortGroup, looking for an installed Emacs;
    - the 6 chasen dictionaries;
    - py-pynds and py27-pynds, looking for boost's Python library;
    - ImageMagick and ImageMagick7, which ask for cryptlib's
      `libCL.dylib` to declare a build conflict;
    - cyrus5-imapd, whose `glob` of `${prefix}/include/db*` was a
      directory enumeration of the host;
    - libunwind, libmemcached, mumps, pflogsumm, scite, tigervnc,
      unixODBC, wap11gui, and two minivmac ports.
  - **rb-rttool** is phase 2's missing-program fix, made after that
    survey.
- **Twelve more ports** had a fetch check become conclusive without
  changing their outcome. Five of them now stop at the checksum probe
  instead.
- **No fetch guard was lost or changed.**
- **Cost:** 32,774 CPU seconds and 69.0 minutes, against phase 2's
  32,104 and 67.8.

Outcomes can't show a port that was conclusive before and plans
differently now. So every Portfile the fresh installation answered for,
other than Base's lookup of `make` (7,387 of them), was evaluated with
phase 2's code and with phase 4's, natively and in the modelled darwin 25
x86_64 context, and the fields compared:

- **No fetch field changed** in any of the 14,774 contexts: version,
  revision, epoch, distfiles, master sites, checksums, distname,
  `dist_subdir`, `worksrcdir`, patchfiles, `extract.only`, the fetch
  type, git's URL and branch, `go.vendors`, and `cargo.crates`.
- **No evaluation newly failed or newly succeeded.**
- **Dependencies changed in 2,830 contexts, each for installed state CI
  doesn't have:**
  - 2,820 drop `depends_extract bin:lbzip2`. Base's `portextract` adds
    it whenever `findBinary lbzip2` finds one, and one is installed
    here;
  - py-pynds and PlasmaClient drop `port:boost`, which they keep only if
    `${prefix}/lib/libboost_python-mt.dylib` already exists;
  - cyrus5-imapd gains `port:db60`, its default when no db is installed;
  - ldns and rpki-client default to LibreSSL rather than OpenSSL, since
    OpenSSL is installed here.

## `with-deps` isn't needed

The design builds `with-deps` (phase 4b) only if registry answers reach
what a bump edits. The ledger now names every question: 93 subjects,
asked by 897 subports. Besides the 186 Portfiles asking through
`qt5_version_info`, only 47 ask the registry while they are evaluated,
and each answer goes elsewhere:

- `qt5_version_info` picks the Qt5 flavour, for dependencies and paths.
  CI's second evaluation would find installed the flavour the first
  chose, so `with-deps` answers as `fresh` does.
- `active_variants` and `require_active_variants` (16 Portfiles) choose
  MPI include directories, keep variants consistent with a dependency's,
  or check them.
- star, smake, and cdrtools check a conflicting cdrtools; qt5 checks its
  own modules; py-automat, py-incremental, py-qt4py, py-qtpy, and privoxy
  add notes or turn tests off.
- gnupg2, ldns, and rpki-client choose default variants, and the
  variants only switch dependencies.
- the elisp PortGroup chooses the Emacs to build with.
- openbabel2 calls `registry_deactivate_composite` at top level when
  openbabel is installed. The fresh answer keeps it from getting that
  far, and phase 2 would refuse it if it did.

With no fetch field depending on the registry at this commit, 4b waits
until a survey shows one that does. The ledger's subjects are how a
survey would.
