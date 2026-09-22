# 2026-09-22: a host that is not a Mac models one

Dockhand refused to start anywhere but a Mac. Every command asks the
MacPorts interpreter what platform it runs on, and on Linux the answer,
`linux 6 x86_64`, is one `macports.PlatformVariables` cannot describe,
so even `bump --dry-run` stopped with "unsupported modeled platform".
Preparing an update and publishing it without a local build needs
nothing a Mac alone has, so a Linux host now describes a Mac instead of
being refused.

## What a Linux host describes

The current macOS on Apple silicon, `eval.DefaultModel`, darwin 25
arm64, with the Command Line Tools for Xcode 26.3 installed and no
Xcode. The evaluator applies it when the interpreter reports a platform
other than Darwin, once, at the start of each session, and reads the
platform back from the interpreter rather than assuming the override
took. `Evaluator.Model` names another release; nothing sets it yet. A
Mac always describes itself, and nothing on a Mac changes.

The current release is one fact, `macos.CurrentDarwin`: the newest
macOS MacPorts builds on in its own CI, which Tart already used as its
default for the same reason. `tart.DefaultDarwin` now reads it.

## The toolchain is the hard part

Setting the platform variables was not enough. MacPorts picks a port's
compiler by asking the host: whether `/usr/bin/clang` exists, and which
Apple clang it is, by running it. On a Linux host with clang installed
the second question fails outright, "couldn't determine build number of
compiler /usr/bin/clang", and Base's own `portindex -p` fails the same
way there, so this is not something dockhand introduced. On a host
without clang it quietly succeeds with no Apple compiler, and every port
that compiles gains a `port:clang-NN` build dependency a Mac would not
have. `use_xcode`'s default asks the host too, whether the tools' `make`
is executable, and answered yes on every port.

`macports.ModelVariables` adds the toolchain to the platform's pairs:
`developer_dir` at `/Library/Developer/CommandLineTools`, `xcodeversion`
none, `xcodecltversion` 26.3, and MacPorts' own compiler cache with
Apple clang 1700.6.4.2 for both paths Base looks at. That build is the
tools' own `clang --version` for Xcode 26.3; it is recorded in
`macos.CurrentToolchain` beside the release. In an evaluator session a
worker hook also answers `file exists`, `executable`, and `isfile` for
the tools' own programs, so a Linux host with or without clang models
the same Mac: Apple's clang, no extra dependency, `use_xcode` 0. The
test was run a second time with clang hidden in a private mount
namespace, and passed.

The indexer is a separate process the hook cannot reach, so it gets the
variables only. With `/usr/bin/clang` present it indexes as a Mac does;
without it, ports indexed locally list a MacPorts clang build
dependency. Most of an index comes from the mirror's seed, and only
dependent discovery reads those dependencies.

## Everything a modeled host observes is a model

`macports.Runtime` now carries `Host`, the platform MacPorts actually
runs on, beside `Platform`, the one it describes, and `Modeled()` says
whether they differ. A modeled runtime's observations are all modeled,
native ones included: its host reads are reported as problems, as a
modeled context's always were, because a Linux filesystem is not what a
Mac would answer. Tart setup refuses a modeled host, since an image is
provisioned on the Mac that runs it.

## Evidence

With MacPorts Base 2.12.6 built on Linux and on the PATH, the suite had
57 failures across seven packages before the change, every one from
assuming a Mac. After it, none. The suite also passes without MacPorts,
as root and as an unprivileged user. A patch-wording assertion in
`portedit` that the MacPorts-backed run reached for the first time takes
either `patch`'s words, as `patchcheck`'s already did.

New tests: `ModelVariables` against the platform's pairs and the
toolchain, `Runtime.Modeled`, and a live test that runs only on a host
that is not a Mac, evaluating a port with `compiler.blacklist-append
{clang < 1300}` and checking the platform, the compiler, `use_xcode`,
and a second modeled release.
