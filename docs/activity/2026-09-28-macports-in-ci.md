# 2026-09-28: MacPorts in CI

The tests that need MacPorts ran only where `DOCKHAND_TEST_MACPORTS_TCLSH` named an installation, and CI named none. So every test that evaluates a Portfile, builds an index, runs the guest program, or checks a signature as MacPorts does skipped there, the guest program's among them. They ran on this Mac only when a run set the variable, and today's runs hadn't until kept archives.

CI now installs MacPorts Base 2.12.6, the version the evaluator is pinned to, as MacPorts documents:
- the release's package for the runner's macOS, found by its major version in the release's checksum file;
- checked against that file's SHA-256;
- installed with `installer`.

The tests then run with `DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh`. A macOS the release has no package for fails the step by name, rather than skipping the tests quietly.

The job's time limit is 30 minutes, up from 20, for the MacPorts tests and the install.

## Following MacPorts' releases

The person asked for a MacPorts image to save the install. None can be had: GitHub's custom images for hosted runners are Linux and Windows only, on larger runners of a paid organization, and GitHub's macOS image has Homebrew, not MacPorts. Baking one means a self-hosted runner, which GitHub advises against for a public repository. MacPorts' own CI installs the package on every run too.

Looking there showed two things about the step:
- **The pin wasn't one.** MacPorts' package ends by running `port selfupdate`. When a newer release is out, that builds and installs it from source, while the log still says 2.12.6.
- **Most of the install's 26 seconds was that selfupdate** fetching the ports tree, which these tests never read: they evaluate their own fixture trees, and dockhand's `portindex` runs name their own sources. MacPorts' CI writes its configuration first "to prevent the postflight script from spending a lot of time running selfupdate", with a source marked `nosync`, since the package keeps configuration it finds.

The person chose to follow MacPorts' releases rather than pin one, to meet a new release's changes as soon as it's out. So CI now:
- reads the release from `RELEASE_URL`, the file `port selfupdate` reads, and refuses anything that isn't a version;
- installs that release's package, checked as before;
- writes a `sources.conf` whose one source, an empty directory, is `[default,nosync]`, before the package installs;
- prints `port version` after it.

Measured on the first run, `installer` took 4.5 seconds rather than 26.6, and the step 6 rather than 28 to 40. The log says what was tested: "Version: 2.12.6".

A new release in a family the evaluator admits, 2.12.7, is tested the day it's out. A new family, 2.13, is refused by the evaluator until dockhand has been checked against it, so it stops CI until it has. That's the signal, as a hard stop.

The one reliance beyond MacPorts' documented interfaces is that its package keeps configuration it finds, which MacPorts' own CI relies on too. `nosync` is documented, in `sources.conf`.
