# A commit built on someone else's push submits

From the Vx language port session, at 7a38bfff: macports/macports-ports
#35220 was amended on GitHub to 9d02a34 after dockhand pushed 4a05dfe. The
person reset the branch to 9d02a34, committed on top of it (1987fa8), and
its check passed, but submit refused, "someone else pushed to #35220 …
nothing will be pushed over it", with no way on but a push by hand.

A push that adds to the branch keeps what someone else pushed beneath it,
so it isn't pushed over: where the fork's branch, as seen, is the head the
pull request reports and the commit to submit descends from it
(`SubmitPlan.Replaces` false), submit goes on, the push leased to their
commit as every push is to the head it saw. One that would replace it
still holds, and its refusal now names the way on: a commit built on it,
as after `git reset --hard <theirs>`. No `--over <sha>` flag: building on
theirs is the case that keeps their work, and the one a flag would add,
pushing over it, is what the hold is for.
TestSubmitUpdatesThePullRequestAndKeepsAPersonsDescription's last step.
