# dockhand new-user exercise: updating `deno`

## Setup
- `make clean && make` — clean, silent, fast. Good.
- Binary run from the dockhand repo; ports tree at ~/Source/macports-ports via -T.
- Used a fresh --db in scratchpad to simulate zero prior state.

## Frictions (chronological)
1. No `--version` flag. `dockhand --version` -> "unknown flag". A new user reaching for it gets nothing.
2. Top-level help: `amend` and `rebase` have the identical one-liner "Correct a tracked contribution and verify it". Can't tell them apart without opening each.
3. Top-level help is long (a "getting started" section exists, which is good), but there's no single "typical flow" example line, e.g. `dockhand bump <port>`. The `bump` help itself has no Examples section, while `assess` does.
4. Running `dockhand outdated deno` with no `-T` from a non-ports directory: it silently started "Generating full PortIndex; this may take several minutes" against a Go repo, then reported `deno: unknown; portindex: executable produced an incomplete index` and exit 1. Should fail fast with "X is not a ports tree (no PortIndex / no port dirs); pass --tree".
5. With `-T ~/Source/macports-ports`, a single-port `outdated deno` still triggers "Generating full PortIndex; this may take several minutes" even though the tree already has PortIndex + PortIndex.quick. First-run cost is high for a one-port query with no explanation of where the cache lives or how to reuse the tree's own index.
6. The `--prefix` flag explanation says "find port-tclsh on PATH"; the environment prompt referenced /opt/macports-ports/bin/port which doesn't exist (only /opt/macports-test). Not dockhand's fault, noting for the report.
7. Timing: `outdated deno` first run = 5m06s wall, ~900% CPU (full PortIndex, 3m40s). `assess deno` afterwards = 9s. The index cost is paid once but there is no hint up front ("first run builds an index, ~4 min") and no way I could see to reuse the tree's existing PortIndex.
8. `outdated deno` says "current 2.9.6; upstream 2.9.6" although a `v2.9.7` tag exists upstream (no GitHub Release yet). Probably deliberate (releases-only discovery), but the output doesn't say *what* it consulted, so a maintainer who has seen the tag will think dockhand is wrong.
9. (retracted) The "Inspecting committed source <hash>" line is the local HEAD of the ports tree. Still, saying "local HEAD" in the message would save a lookup.
10. `assess` output is rich and readable (per-check passed / not-tested lines). Nice. But "input-found" as a headline verdict is jargon; "ready to try an update" would read better.
11. `setup --check` = 39s, boots a disposable VM clone. Clear, positive output ("Ready verification image dockhand-base-tahoe ..."). Good. Minor: it validates only the host-release image and doesn't list the other images present.
12. `auth status` is crisp and useful.
13. `assess deno --version 2.9.7` -> "candidate-checked" but the output never echoes the resolved version/tag (only "current 2.9.6" and "Resolved release passes ..."). I can't tell from the output whether it resolved v2.9.7 or something else. Should print "candidate 2.9.7 (tag v2.9.7)".
14. `assess` on a version that has a tag but no GitHub Release says "discovery: passed; Supported releases discovery" — yet `outdated` didn't discover 2.9.7. The two commands appear to use different notions of "release"; worth stating in `outdated`'s output/help ("published releases only; tags ignored").
15. `bump --diff` says "Fetching MacPorts master for preview; local commits and working-tree edits are excluded." Good explicit statement. But then "Updating PortIndex for changed source paths" — a second index pass after the 3m40s full one; unclear how long this takes.
16. `bump deno 2.9.7 --diff` failed with `portedit: downloading deno-aarch64-apple-darwin.zip: fetch: HTTP 404` (exit 1, 19s). The tag exists but the GitHub Release isn't published. No URL printed, no hint like "release v2.9.7 has no published assets yet; try later". `assess --version 2.9.7` had said candidate: passed with "remote availability is untested", so assess's optimism and bump's failure are consistent, but a new user needs the bridge sentence.
17. Progress chatter: in one `bump` invocation the trio "Preparing PortIndex; waiting for the shared index cache / Checking cached PortIndex / PortIndex ready; installing into staged source" printed three times. Reads like a loop bug and makes the real signal harder to spot.
18. `bump-revision --help` and `refresh-checksums --help` reuse bump's full long description verbatim, including "Omitting the version selects the newest eligible stable numeric version", which doesn't apply. `bump-revision` has no `--reason` requirement hint either; PR text presumably needs one.
19. `--diff` output is a proper unified diff plus Repository/Branch/Commit/Target header. Very good. 1m35s (versionless) vs 15s (revision) — most of the versionless time was release discovery + index refresh.
20. `status` on an empty db: "Snapshot read at ...Z / No recorded jobs." Fine.
21. Real run `bump-revision deno --reason ... --trace`: before anything is built it hashes the 28 GB Tart image ("may take several minutes"). First-verify cost on top of the first-index cost. A one-time "first run will take ~10 min: building index, hashing image" up front would set expectations.
22. `status`/`status --active`/`status deno` all work mid-run and are informative (phase, target, policy, detail). Nits: (a) ID soup: repo_..., job_..., request_..., change_job_...; (b) trailing line `Change change_job_...: open; branch: ; current revision: ; published revision: ` with empty fields looks broken; (c) `source tree: 9c9024...` in status vs `source commit c12f6a...` in the run log are different hashes for what a user thinks of as one thing.
23. The run log says "Verification provider: tart; no GitHub verification will be submitted" — good, explicit.
24. `--trace` streams every MacPorts `DEBUG:` line (210 of 468 output lines were DEBUG). Useful when something fails, but by default `--trace` should show phases (lint / fetch / install / activate) and hide DEBUG unless asked (`--trace=debug`?).
25. End-to-end `bump-revision deno --trace`: 9m58s wall. Breakdown observed: ~8 min before "admitted" (index refresh + hashing the 28 GB image + VM boot + transfer), ~1 min inside the guest for lint + install + activate. The user-visible summary at the end shows admitted/finished timestamps but not the prepare time, so the 8 minutes of overhead are invisible in the record.
26. Final status block is thorough (verdict, environment sha, MacPorts version, tools) and the run ends with a full `status` dump. Good closure.
27. Ports tree hygiene: dockhand did NOT touch my checkout (HEAD stayed on master, reflog unchanged, untracked files intact). It only added a branch ref `dockhand/revbump/deno-<jobid>`. Excellent.
28. Commit produced: author = my git identity, subject "deno: revbump", body = my --reason + a "Generated-by: dockhand" trailer. Subject is a bit terse vs MacPorts habit ("deno: rebuild for X"); consider putting the reason (or a --title) in the subject.
29. **publish --dry-run plans to push the branch to `macports/macports-ports` (origin) and open a PR from `macports/macports-ports:dockhand/...`.** In this tree `origin` is the upstream repo and the fork is a differently named remote (`herby`). This is a very common maintainer layout. Dockhand should detect origin == upstream and either refuse with "pass --remote <fork>" or auto-pick the remote whose URL matches the authenticated GitHub user (it already knows I'm herbygillot). For a committer this would push a stray branch to the main repo.
30. PR body from --dry-run is nice: filled-in MacPorts PR template, "Tested on" block, explicit unchecked items with reasons ("Port declares no test phase"). Good.
31. `abandon deno` works and is clear ("branch, evidence, and any remote PR are preserved"). There's no `--delete-branch` option and `gc` doesn't obviously cover local branches; after a few exercises the tree accumulates `dockhand/bump/*` branches (there were 8 stale ones in this tree already).
32. Positive: `--json` is available on everything; `status` is fast (<1s); Ctrl-C semantics are documented in help ("detaches without canceling").

## What a new user could not do
- An actual version update of deno: upstream had only a tag (v2.9.7) with no released assets, so dockhand correctly could not fetch archives. The failure message was the weak point, not the behavior.

## Top 5 improvements, ranked
1. Publish remote selection: never default to pushing to the upstream repo; detect fork vs upstream by URL/auth user.
2. Fail fast when `--tree` isn't a ports tree (today: multi-minute index build, then "incomplete index").
3. First-run cost transparency: announce index build (~4 min) and image hashing (~minutes) up front; consider reusing the tree's existing PortIndex.
4. Error messages with context: the 404 should print the URL and say the release has no assets; `assess --version` should echo the resolved version.
5. Help text hygiene: distinct one-liners for amend/rebase; bump-revision/refresh-checksums shouldn't reuse bump's version-selection prose; add an Examples block to bump; add --version.
