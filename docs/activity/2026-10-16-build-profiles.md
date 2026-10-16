# Build profiles (v0.4.0 part 2, item 1, first half)

The person's design of 2026-10-13: a build profile is a provider and the
releases it builds on, a list or a rule, and checks build where one says.

- **config:** `[build]` with `default`, `unattended`, and `profiles`, as
  `ci-exact = { provider = "github", releases = "macports-ci" }` or
  `mine = { releases = ["15", "26"] }`. Load refuses a provider dockhand
  doesn't have, releases that are neither a list nor a rule, a name one of
  dockhand's profiles has, a release list for github or command, and a
  default naming no profile. `check.on` is gone with no alias, as the
  person said: a file that still sets it is refused with what took its
  place.
- **engine** (`engine/profile.go`): `Profile`, `Builds`, and `Where`, the
  resolver the command package review called `CheckDefaults`. `--on`'s
  explicit environments come first; else `--build`'s profile; else
  `build.unattended`'s for a check nobody looks over, and `build.default`'s
  for the rest. dockhand's own profiles all run on Tart: `host` (alias
  `local`), this Mac's release; `minmax`; `macports-ci`, what `--on ci`
  read, the releases MacPorts' workflow at master names; and `all`, every
  release with an image. `command` is a profile where the person's script
  is set up. `build.default` is `host` unless set, or `command` where the
  script is set up, as before; `build.unattended` is `minmax` where Tart is
  set up, and else `build.default`'s, so an Intel Mac or a script alone
  still checks unattended. `--on ci` is refused, naming `--build
  macports-ci`.
- **minmax** plans each port on its oldest and newest release of the
  environments it builds in, macOS 12 at the earliest, with what it needs
  first there (`planning.Input.Span`, read in `Exclusions`): a run of
  several ports covers the union of theirs, and each port's other releases
  say "minmax builds it on its oldest and newest releases, macOS 12 and
  26". A release its Portfile's platforms exclude keeps its own reason.
- **Where it's read:** `check --build <profile>`, which conflicts with
  `--on`, now explicit environments only; the plan keeps its profile
  (`Plan.Profile`, in check's plan line and `--json`); bump's and serve's
  checks take `build.unattended`; the evidence still requires
  `build.default`'s environments, as it did `check.on`'s. `config` shows
  `build.default`, `build.unattended`, and each profile, from the defaults
  table. Submit's preview names `--build macports-ci` for the CI releases
  no check built.
- **Docs and rows:** usage.md's "Where it builds" and settings,
  tart-provider.md, command-provider.md, github-provider.md,
  architecture.md, the README, and row B9, whose github default is a
  profile now.
- **Left for the second half:** the check-standing verdict,
  `CheckStanding(profile)` on `BranchStatus` and `SubmitPlan`, which
  status, submit, and `PassingBranches` read alike.
