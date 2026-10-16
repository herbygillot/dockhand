# A command in a checkout dockhand doesn't track

The roadmap's part 3 item, by the command-line UX review's §2 rule, a
branch started where nothing else could be meant.

- `engine.Current` refuses with typed errors, both still `ErrNoBranch`:
  `UntrackedError{Name}`, a Git branch dockhand doesn't track, and
  `DetachedError`, a checkout at a commit on no branch.
- `engine.StartAtHead`: at a detached HEAD, a branch named for the first
  port its commits change from master, as `NameFor` names one, made with
  `git switch -c` (`git.Repository.SwitchNew`) and tracked as adopt
  tracks it. Commits that change no port are refused, since nothing names
  a branch for them.
- Every branch command that resolves the checkout (`branchFlags.resolve`,
  check among them), with no -b, -p, or --pr, offers on a terminal what
  tracks it: adopt for a Git branch someone made, and a branch started at
  HEAD for a detached one, then goes on. A script is refused in one
  sentence that names the command: "dockhand adopt my-jq tracks it, then
  run this again", or `git switch -c dockhand/<name>` and adopt.
- status reads the checkout itself, and its refusal is unchanged.
