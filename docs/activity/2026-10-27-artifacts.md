# dockhand artifacts prints a check's built archives

2026-10-27, the last of field testing's outdated-ports block (roadmap
76f07f2c), at the person's "#8: I think `dockhand artifacts` is a yes.",
for otool and smoke tests by hand. A passed target's archive is kept by
its digest in the archive directory (engine/archives.go's `archivePath`),
named nowhere a person saw.

- `Engine.Artifacts` reads a check's evidence: for each port and platform
  it passed on, the archive's name and path where it's kept, or why not,
  "its archive wasn't kept" or "its archive is gone, as cleanup removes
  one no check uses". A target that didn't pass built none.
- `dockhand artifacts [branch]` (runcmd's `ArtifactsCommand`, in the Check
  group beside logs) prints a line for each, the port, the platform, and
  the path, of the branch's latest check, the worktree's where none is
  named, with `-b`, `-p`, `--pr`, and `--json`. It reads only, so it has
  no `--plan` (the contract test's noPlan). docs/usage.md says it.

TestACheckSaysWhereItsArchivesAre: a check that passed on two releases
gives each passed port's two archives, harbor-viewer's failure none, and a
removed archive says it's gone.
