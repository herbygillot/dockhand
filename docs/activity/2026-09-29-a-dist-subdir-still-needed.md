# 2026-09-29: a dist_subdir an archive still needs stays

The hugo exercise's yq run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#yq-where-dockhand-removed-a-line-the-port-needed), findings 1 and 2) found `update` removing a line the port needed. Every version update removed a top-level `dist_subdir ${name}/${version}_${revision}`, or `${name}/${version}_N`, the forms MacPorts' guide gives a stealth update, saying the new version's archive had a name of its own. It never looked at the archives. yq's second distfile, its man page, is `yq_man_page_only.tar.gz` in every version, and the line, added by hand for colliding man pages, keeps the versions apart on the mirrors. Without it, check-27 failed at checksum on Tart: a mirror served the old man page under the shared name. GitHub's check passed, having fetched it from GitHub, so a check on GitHub alone would have let the update be submitted without the line.

Now the removal needs every archive of the new version to have a name of its own. The names are MacPorts' own fetch plan's for the Portfile as it stands (`shippedPlan`), and the new version's downloads' (`sharedDistfile`). Where any name is the same in both, the line stays, unchanged, and the new version gets its own directory. Where the current archives can't be told, it stays too. That case can't be reached in an update, whose own fetch the same policy governs, so it isn't tested.

`update --plan` now says it would remove the line, "Removes dist_subdir: …", as the update says it did (finding 2); the plan had shown only the diff's `-` line. The guide and the design say when the line goes.

Tests:
- `TestANewVersionKeepsADistSubdirAnArchiveStillNeeds`, a versioned archive beside a man page whose name stays;
- `TestANewVersionDropsTheStealthDistSubdirAsEvaluated`, whose fixture's archive now has a versioned name, since its `fixture.zip` kept one name across versions, as yq's man page does;
- the command's stealth test, for the plan's words.

Four mutations each fail a test.
