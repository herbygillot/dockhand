# Branches: adopt's uncommitted ports, a merged branch from its record, archive's ignored files (v0.4.0 part 1, c)

The Branches group's first three items.

- **adopt names the ports the working tree changes.** "0 commits above
  master …, changing no ports yet" was said of a tree with edits to three
  ports: `describeScope` read only what's committed. `Adoption.Uncommitted`
  (`uncommittedPorts`) are the ports the worktree's uncommitted edits
  change that its commits don't, and adopt says them, "with edits not yet
  committed to gcc15, libgcc15, and gcc15-libcxx"; `--json` has
  `uncommitted`. `describeScope` lists with `prose.And`, as the
  coalescing asked.
- **A merged branch's name is said from its record** (the rc8 full
  stage's B6, the rc10 full stage's C7). `Resolve` found open, closed, and
  archived branches, and a merged one read as a name nothing has, "no
  tracked branch named …", where `status --all` showed it. Its not-found
  error now says what became of the merged one (`mergedWords`): "jq-update
  was merged as #34901 and cleaned", or, where clean kept it, that it
  takes no changes, where the commits past the merge are kept, and that
  `dockhand adopt <name>` tracks them as a new branch. `path`, which only
  reads, resolves the record (`ResolveRecord`): it gives a kept merged
  branch's worktree, and makes none again for one cleaned. A closed or
  archived branch was already found by name.
- **archive says which ignored files its worktree removal deletes.** `git
  worktree remove --force` deletes them unlisted; archive lists them first
  (`git.IgnoredFiles`, `ls-files --others --ignored --exclude-standard
  --directory`) and says them, "Its ignored files went with it: build.log";
  `--json` has `ignored_removed`.

The name lookup across states and SQL-S3's optional index weren't needed:
the merged lookup is the one `ResolveRecord` already had.

Tests: TestInitStartPathAndAdopt (the uncommitted ports),
TestAMergedBranchIsSaidFromItsRecord, TestArchiveTakesTheWorktreeAndPathBringsItBack
(the ignored file).
