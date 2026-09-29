# 2026-09-29: plans beside a branch dockhand doesn't track

The person decided D7 on 2026-09-29, from the chezmoi run's finding 3. `update chezmoi --plan`, run while the person's own `git-devel-2.56.0` was checked out, refused with "git-devel-2.56.0 is not tracked; dockhand adopt tracks it", though nothing on that branch touched chezmoi. A plan changes nothing, and on master it already planned from master.

Now the question is whether what's checked out here changes the port, which the engine answers (`Engine.ChangesHere`): the commits since the checkout left master, as fetched now, and the files edited or added and not committed, in the port's directory. It's measured as dockhand's own branches are (`ScopeOf`), from where the checkout left master, so master's own progress since isn't counted.

- **An untracked branch that doesn't change the port** is no context, as master isn't. `update --plan` plans on master, and says why: "Planned on master 1a2b3c4 (fetched just now), since mine doesn't change jq". `update` without `--plan` starts a branch as it would on master, asking on a terminal, and otherwise saying "jq is in no open branch, and mine, checked out here, doesn't change it; start one with --new, or name one with --branch <name>".
- **One that does** is still the person's to adopt. The plan refuses, "mine changes jq, which a plan on master would leave out; dockhand adopt tracks it, so the plan reads its changes, or --new --plan plans on master without them". An update refuses in the same terms.
- **Master's own edits to the port,** not committed, would be left out of a plan on master too, so the plan refuses them and names `--new --plan`.

The guide says so, under `update`.

`TestAnUntrackedBranchHereIsTheirsToAdopt` covers a branch that doesn't change the port, and each way one can: a file it adds, an edit not committed, and a commit. `TestAPlanOnMasterDoesntLeaveEditsOut` covers master's edits. Ten mutations each fail a test.
