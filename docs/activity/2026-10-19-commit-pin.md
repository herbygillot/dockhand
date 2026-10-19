# update moves a commit pin

The roadmap's v0.5.0 part 3, step 2, its commit pin. wezterm pins
github.setup to a commit, and its version is the commit's date and hash,
`20261005-372548-295b0b25`. outdated said to edit it by hand.
`update wezterm` crashed: upstream's `resolve` dereferenced the nil release
that a branch's head gives, a nil pointer panic at resolve.go:42, on main
and so on rc5's commit. A version or commit named was refused.

- upstream's `pinned` reads a port that pins a forge's source to a whole
  commit and tracks a branch (`livecheck.type git`). `resolvePin` takes the
  branch's head by `git ls-remote`, as discovery reads it, or a whole
  commit named, which the forge must have (`HasCommit`). An abbreviated
  commit is refused, saying to name it whole.
- The version is the port's scheme where its version reads as the pinned
  commit's date and pieces of its hash (`pinnedVersion`). The dates come
  from the forge (`DatedRepository.CommitTime`), and the date can be
  written as 20060102, 2006.01.02, 2006-01-02, or 2006.1.2. Otherwise a
  version named (`update <port> <version>`) is taken, with the head.
- `model.Release.Pinned` marks the release. Its tag and commit are the
  commit, and its source version is the commit too. `Check` holds it to the
  Portfile's pin and the forge's commit (`checkPin`).
- A branch's head with no release, for a port that isn't pinned so, is now
  an error saying to name a version, where it was a panic.
- portedit writes the port's own version literal with the commit
  (`portfile.SetVersionLiteral`, one `version` command of that literal).
  The probe's release check carries the release, so a pinned release is
  checked as it's edited. `planGitVersion` takes a git.branch the version
  edit already moved, as github.setup carries it.
- The release is said as "GitHub commit e2a98e1", not as a tag.
- outdated's row says `dockhand update <port>` for a moved pin, and its
  count says "for update", where both said to edit by hand.
- Replayed in a scratch tree: `update wezterm --new --plan`, which
  panicked, plans `github.setup wez wezterm e2a98e1b...` and `version
  20261010-e2a98e-1b8af156`, with the upstream comparison read from the
  forge's archive of each commit. `outdated wezterm` names `dockhand update
  wezterm`. Layer 1's set's `update --plan`s are the same.
- Tests: `TestAPinnedVersionFollowsItsCommitsDateAndHash` (upstream) and
  `TestSetVersionLiteralWritesTheOneVersion` (portfile).
