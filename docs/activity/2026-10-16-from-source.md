# Saying which dependencies build from source

The roadmap's part 3 item (decided 2026-10-07): before a target builds,
say which of its dependencies have no binary archive for the guest, since
those build from source and take the time.

- The Tart guest program, before installing a target's dependencies, asks
  each one not yet installed, with what it needs (`port -q rdeps`), for its
  binary archive alone: `port -N -b archivefetch <port>`. port(1) documents
  `-b` as binary-only, failing where no archive is available, and
  `archivefetch` fetches the archive install would fetch anyway, so the
  ones with archives cost nothing more. Those that fail are what install
  builds from source (`archiveless`).
- They're said in the target's log, "dockhand: no binary archive for rust,
  cargo on this release; install builds them from source", and in the
  results as `from_source`, beside `building` and `phase`, cleared with
  them between targets; the guest writes them at once.
- Tart's progress line carries them: "building jq (2 of 5): dependencies;
  rust, cargo: no archive for macOS 27 arm64; building from source", said
  once, as it changes.
- No HEAD of packages.macports.org from the host: the guest's answer is
  the release's, architecture's, and variants' own, which a host guess
  isn't. The guest program's pin moves; the verifier protocol stays, as
  nothing builds or judges a port otherwise.
