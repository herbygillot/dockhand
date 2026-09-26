# 2026-09-26: a release is planned with the tools it builds with

Before this change, a release building with Xcode was still planned in the
Command Line Tools profile, and this Mac's own release was planned with
whatever this Mac has: Xcode 27.0 here, while Tahoe's image builds with
26.6. A Portfile that chooses by `xcodeversion` could plan one way and build
another. Phase 5's note found 15 Portfiles whose patch files differ by
profile, and guards such as php's and gcc8's. The person asked for the mismatch to be fixed.

## What changed

- **The toolchain model takes the developer tools.** `macports.Toolchain`,
  `ModelVariables`, and `ToolchainAnswers` take them.
  - With Xcode, the model is the facts table's Xcode row: a Tart Xcode
    image's where there is one, else a buildbot's. Its developer
    directory is Xcode's, where xcode-select points.
  - A release without an Xcode row has no toolchain with Xcode, and is
    described alone, as a release with no row always was.
  - What a row doesn't tell, its clang and SDKs, is taken only from a
    Tart row with the same Xcode and tools. Before, a tools-profile row
    could borrow from an Xcode image's row with the same tools package.
  - `/usr/bin`'s shims answer with Xcode too, not only with the tools.
- **An observation can state its tools**
  (`ObservationRequest.DeveloperTools`). That models the context even on
  the Mac's own release, rather than reading this Mac's Xcode.
- **The planner reads each directory in an environment.**
  `PortReader.Ports` takes the `Environment`, so its tools as well as its
  platform. A target's `NeedsXcode` is kept per environment, since the
  tools change the answer.
- **Drift covers Xcode.** The guest records Xcode's version where it has
  Xcode (`xcodebuild -version`; the base images refuse it, and record
  none). The drift report compares the guest with the row the plan was
  read with: the Xcode row, Xcode's version included, for an Xcode image.
  So setup with a newer `.xip` than the harvest saw is reported, and the
  facts table can be harvested again.
- **Left as it was:**
  - The port index staged into a guest describes only the platform on a
    Mac, as MacPorts' buildbot's does, so it's the same whatever the
    tools.
  - The oracle's own contexts for authoring, which state no tools, keep
    the tools profile and the Mac's own release.

## Tests

- **The facts table's Xcode profile:**
  - Tahoe from its Tart Xcode image, with Xcode's developer directory
    and its clang in the compiler cache;
  - Big Sur from its buildbot;
  - none for Darwin 9.
- **With real MacPorts:** `xtools`, known to fail when `xcodeversion` is
  none, is excluded with the Command Line Tools and planned with Xcode,
  on Sequoia, modelled, and on Tahoe, this Mac's own release.
  - Blanking the environment's tools in the port reader makes the test
    fail on Tahoe, where this Mac's Xcode had answered.
- **Drift:** an Xcode image's Xcode compared with the Xcode row.
- **The guest program's tests pass with its new line.**

## Proven on the Mac

The scratch branch revbumping `ssh-askpass-mac`:

- **`check --plan --on tart:tahoe,sequoia,monterey --also tree --also
  pv`:**
  - all three releases planned "with Xcode", each modelled from its Xcode
    row, Tahoe's too;
  - ssh-askpass-mac needs Xcode in each, and tree and pv in none;
  - nothing unmet.
- **`check --on tahoe --also tree` passed** in a clone of
  `dockhand-xcode-tahoe`, which was deleted after.
  - MacPorts in the guest saw "Xcode 26.6, CLT 26.6.0.0.1781586589",
    exactly the Xcode row's, so no drift was reported, rightly.
  - The guest's own record of its Xcode wasn't seen directly, since its
    results stay in the clone. The drift test covers the comparison.
