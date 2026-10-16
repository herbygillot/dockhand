# The hidden aliases deleted

Part 1's hygiene, first step, after PR #4 merged (62b957be), by the UX
thread's answer on the hidden aliases (ux-review/aliases-and-bump-2026-10-15.md):
each was a second spelling of what `setup` or `undo` does, and none is
surfaced.

- **`init`** goes; `setup --yes` is the same, offering nothing. `runSetup`
  loses its `offer` parameter, which only `init` set false.
- **`restore <checkpoint>`** goes; `undo <checkpoint>` calls the same
  `restoreCheckpoint`.
- **`providers`** and **`providers setup tart`** go; setup's Providers rows
  are `providerLines`, and `setup tart` is the same constructor. The
  providers tests read `providerLines` through a helper, `providers(t)`,
  and the setup ones run `setup tart`.
- **`clean --archived`** goes; `archive` has taken the worktree itself since
  22b4a26, and the one run on the person's Mac (v0.3.1's `clean --archived
  --plan`, 2026-10-15) listed only branches archived before that. Naming
  an archived branch, `clean <name>`, still takes a worktree `path`
  checked out again; the archive test does that now. `cleanStates` takes
  merged and closed.
- **Old spellings are pointed, not run.** `setup` has `SuggestFor` init,
  auth, and providers, and `undo` restore. The root's argument check was
  `cobra.NoArgs`, which leaves cobra's suggestions out of its refusal, so
  `unknownCommand` gives "unknown command "init" for "dockhand"" with
  "Did you mean this? setup", which a typo of any command gets too.
- **Docs and rows:** tart-provider.md, usage.md's clean section,
  architecture.md, and design-v3.md's first-run sketch, tidy line, and
  checkpoint line name `setup`, `setup tart`, and `undo`; acceptance rows
  A2 (whose `setup` already ran), F7, and B9 run `setup --yes` for the
  Providers rows, A12 and quick.sh's `--image` run `setup tart`.
  design-v3.md's v2-to-v3 table keeps the old names on purpose. The README's
  v0.3.1 known issue naming `auth status` stays, as v0.3.1 has it.
