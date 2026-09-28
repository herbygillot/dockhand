# 2026-09-28: MacPorts in CI

The tests that need MacPorts ran only where `DOCKHAND_TEST_MACPORTS_TCLSH` named an installation, and CI named none. So every test that evaluates a Portfile, builds an index, runs the guest program, or checks a signature as MacPorts does skipped there, the guest program's among them. They ran on this Mac only when a run set the variable, and today's runs hadn't until kept archives.

CI now installs MacPorts Base 2.12.6, the version the evaluator is pinned to, as MacPorts documents:
- the release's package for the runner's macOS, found by its major version in the release's checksum file;
- checked against that file's SHA-256;
- installed with `installer`.

The tests then run with `DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh`. A macOS the release has no package for fails the step by name, rather than skipping the tests quietly.

The job's time limit is 30 minutes, up from 20, for the MacPorts tests and the install.
