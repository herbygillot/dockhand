# Flags

Every flag of `update`, `bump`, `ship`, `submit`, `check`, `edit` and `revbump`: what keeps it, which commands take it, the stage that reads it, what leaving it out means, what false or empty means, how long it lasts, and what can become of it. A flag another part of dockhand owns names it. This page is made from dockhand's own table; a test fails where the two differ.

| Flag | Kept in | Taken by | Read at | Left out | False or empty | Lasts | Outcomes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `[version]` | Targets[].Named | update, bump; ship through a pinned target | edit | the newest release, or the one kept |  | with the target's release, on the request | done, satisfied, refused |
| `--shared-release` | Shared | update, bump, ship | edit | each port its own release | as left out | the request | done; satisfied with no sibling |
| `--keep-old-checksums` | Source.KeepOldChecksums | update, bump, ship | edit | false | as left out | the request | done, satisfied |
| `--keep-modeline` | Source.KeepModeline | update, bump, ship, revbump; serve by setting | edit | the setting | false, over a setting or a kept true | the request | done, satisfied |
| `--keep-http` | Source.KeepHTTP | update, bump, ship; serve by setting | edit | update.keep_http | false, over the setting | the request; the edit's HTTPS | done, satisfied |
| `--source` | Source.Source | update, bump, ship; serve by setting | edit | update.source, applied when a version edit is made |  | the request; the edit's Source | done, satisfied, refused |
| `--follow-moves` | Source.FollowMoves | update, bump, ship; serve by the port's moves | edit | the port's moves | don't follow | the request | done, satisfied |
| `--with-obsolete` | Actions.WithObsolete | update, bump, ship | edit | the stub is left, and said to lag | as left out | the request | done; satisfied with no stub |
| `--revbump-dependents` | Actions.RevbumpDependents | update, bump, ship | edit | none bumped | as left out | the request | done; satisfied with none |
| `--except` | Actions.Except | update, bump, ship | edit | none left out | as left out | the request; the edit's Excepted | done; refused alone |
| `--closes` | Actions.Closes | update, bump, ship | edit | none |  | the branch's Closes | done, satisfied; refused with nothing else to change |
| `--hand-edit` | none: the attempt's own | update | edit |  |  | the hand edit's record | as today |
| `--branch`, `--new`, `--port`, `--pr` | none: they choose the branch | update, ship, submit, check, edit, revbump | before the request | the port's branch, started where none is |  | the command | done, refused |
| `--mine`, `--maintainer`, `--category`, `--not-maintainer`, `--passing` | none: they make targets | update, ship | before the request |  |  | the command | targets, or none |
| `--over-allowance` | none: the lookup's | update, ship | before the request | false |  | the command | as today |
| `--to` | To | ship; bump as --to submit | every stage | what is kept, else submit |  | the request, where given | done; kept; replaced by a later one given |
| `--yes` | none: the caller's | update, ship, submit, check; bump fixed | every stage | a person answers |  | the command | as today |
| `--on`, `--build` | Check.On, Check.Build: one place | bump, ship, check | the check | the place kept, else the settings of that moment |  | the request; either replaces the other | done; kept below the check; not read by --baseline |
| `--tests` | Check.Tests | bump, ship, check | the check | what is kept, else the settings of that moment |  | the request | done; kept below the check; not read by --baseline |
| `--draft`, `--type`, `--skip-notification` | Publish.Draft, Publish.Type, Publish.SkipNotification | bump, ship, submit | submit | what is kept, else not a draft, the type worked out, notified | false, over a kept true | the request, then the publication | done, kept |
| `--title`, `--note`, `--no-note` | the branch's own | bump, ship, submit | submit | what the branch holds, else worked out | an empty --title clears it; an empty --note is refused and --no-note clears it | the branch, saved with the request | done, kept |
| `--tested-binaries`, `--tested-variants` | Publish.TestedBinaries, Publish.TestedVariants as asked, then the branch's own | bump, ship, submit | submit | what the branch holds for these files | false withdraws it, pending or bound | the request until bound to its files, then the branch | done, kept |
| `--no-check` | Publish.NoCheck | submit | submit | what is kept, else a check | false, over a kept true | the request, then the publication | done, kept |
| `--ready`, `--accept`, `--standing`, `--remote`, `--rewrite-description` | submit's own | submit | submit |  |  |  | ;owned by the publication record (#191); a continued submit carries --draft, --no-check, --type and --skip-notification, a flag left out told from false |
| `--head` | submit's and check's own | submit, check | submit; the check |  |  |  | ;owned by the publication record (#191) and the check lifecycle (#193) |
| `--fresh`, `--only`, `--also`, `--variants`, `--keep`, `--staged`, `--working-tree`, `--include`, `--replace`, `--baseline`, `--of`, `--enqueue`, `--for` | check's own | check | the check |  |  |  | ;owned by the check lifecycle (#193) |
| `--plan` | none | update, bump, ship, check, submit, revbump |  |  |  |  | writes nothing |
| `--subject`, `--no-open` | a primitive's own | revbump, edit |  |  |  |  | as today |
| `--tree`, `--db`, `--tool`, `--json`, `--verbose`, `--migrate-database` | settings' | all |  |  |  |  | ;owned by settings (--migrate-database: #281) |
