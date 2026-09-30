# 2026-09-29: said before acting

Batch 6 of the roadmap's smaller items: what a person needs to hear before a step that's hard to take back.

**Other open pull requests, from `update`** (the libuv run's finding 1). libuv 1.53.0 was planned without a word of #34620, already open for it; only submit's preview looked. An update, planned or made, now names the port's other open pull requests, "Also open for libuv: #34620 …", and in its JSON as `others`, or says why it couldn't look. As decided today, it stops for none: submit shows them again, and `bump` and serve hold on one. The search is submit's own, now shared (`openPullRequests`), leaving out the branch's own pull request. It runs only where the command asks (`UpdateRequest.LookForOthers`), a person's update, so serve's many updates, which hold at submission anyway, don't search twice. The command tests' world now answers GitHub with a fake that knows of none, since an update's search would otherwise have reached GitHub from a test.

**A dependent before its dependency, in `tidy --group`** (the libuv run's finding 6). `--group "2 1"` would have committed sqlit-tui, which pins textual-fastdatatable 0.19.0, before the commit that provides it. `Engine.Regroup` now rearranges the plan as before, then notes on each commit that comes before one it depends on, "comes before commit 2, which changes libharbor, a port jq depends on as check-3 orders them", from the newest check of the files. Commits are by directory and targets by port, so each target is placed by its directory in the check's plan: sqlit-tui depends on py313-textual-fastdatatable, whose directory is py-textual-fastdatatable's. The note warns; the order is the person's. Files no check has seen are rearranged without it.

**A check of the files still to finish** (the sshuttle run). Submit said "run dockhand check first" where a check of the commit's files was queued or running. It now names that check: "check-7, of this commit's files, is queued; dockhand wait check-7, then submit, or submit a draft with --draft". The runs of a tree in given states are one query (`runsOfTree`), which the finished-checks one now uses.

**A migration, said and kept** (the certigo run). A newer dockhand migrated the database silently, and older builds can't open it afterward. As decided today, before migrating, the database is copied beside itself as it is, `dockhand.db.schema-24`, with SQLite's `VACUUM INTO`, which makes a consistent copy and runs outside a transaction, so it comes before the migration's. Where the copy can't be made, nothing is migrated. The migration is then said: "Migrated dockhand's database from schema 24 to 25; dockhand builds older than this one can't open it now. Its schema 24 form is kept at …". Copies kept by earlier migrations go once they're 30 days old, the one just made staying.

Tests:
- `TestAnUpdateNamesOtherOpenPullRequests`, in the command: planned, made, in JSON, and a search that fails; `TestOtherOpenPullRequestsLeaveOutTheBranchsOwn`;
- `TestARegroupPuttingADependentFirstIsNoted`;
- `TestASubmissionNamesTheCheckItWaitsOn`;
- `TestAMigrationKeepsTheDatabaseAsItWas`: the copy is the database before, private, an old copy goes, and neither a current nor a new database is copied.

Ten mutations each fail a test, after one test was added for a survivor. Another survivor was a dead condition, `version == 0`, which an empty database never reaches since it has no application ID yet; it's gone.
