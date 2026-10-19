# ship chooses a port's branch through Select, and Select's work is typed

The roadmap's v0.5.0 part 1 item 4, "One owner for each rule": the
architecture review of 5897a82's R3-3 with R3-25.

## R3-3: one 0, 1, or many rule

`shipTargets` re-implemented the port-to-branch rule on
`BranchesChanging`, with its own refusal ("Use dockhand ship -b
<branch>"), the day after `Select` took it; author.go repeated two of
Select's cases.

- `shipTargets` calls `e.Select` with `SelectExisting` and
  `settleSelection`: none is a branch the update starts, one is resumed,
  several are asked about on a terminal (a new branch among them) and
  named to a script by `AmbiguousError`, as every authoring command does.
- `settleSelection`'s "n for a new branch" path asked `NameFor` and
  `Start` itself; it now asks Select for `--new` and settles that, so an
  update's new branch is named for its version as `--new` names it.
- The untracked-checkout rule ("a branch a person made, not master or
  main") is `Engine.Untracked`, and with `ChangesHere`,
  `Engine.UntrackedChanging`, which Select reads. Its copies went:
  author.go's `untrackedHere` (read by `unbranchedPlan`) and verbs.go's in
  `revbumpBranch`. `unbranchedPlan` keeps its own plan wording, and reads
  master's own changes too, which Select doesn't; its last step, "no open
  branch changes the port", is Select's `SelectExisting` answer.
- `startFor` asks Select for `--new` rather than naming the branch itself.
- Call sites switched: `shipTargets`, `settleSelection`, `unbranchedPlan`,
  `startFor`, `revbumpBranch`.
- Grep for the old shapes: `BranchesChanging(` in command finds none;
  `!= "master" && .* != "main"` is in `Engine.Untracked` only.

## R3-25: typed work

`Select(ctx, selector, what, purpose string)` took a name suffix, the
sentinel `"version"`, and one of five purpose strings that `whatFor`
mapped; `settleSelection` passed `errStartForVersion` between two command
functions as control flow.

- `engine.Work` (`WorkNone`, `WorkUpdate`, `WorkEdit`, `WorkChecksums`,
  `WorkRevbump`, `WorkCreate`) is what Select takes; it names the branch
  (`nameSuffix`) and is the word a refusal says.
- `Selection.ForVersion` says an update's branch is named for the version
  it finds; `chooseBranch` and `settleSelection` return a `chosenBranch`
  whose `forVersion` author reads.
- `whatFor` and `errStartForVersion` are gone; `author` takes a Work.
- "--new starts another" is said only where the command has `--new`
  (`say`), so ship doesn't offer it.

Tests: `TestShipChoosesAPortsBranchAsSelectDoes` (command) and
`TestTheWorkNamesTheBranchStartedForIt` (engine).
