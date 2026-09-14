# Follow-up contributor and lifecycle exercise

Committed the introductory README, four croc fixes, and the original activity report in six focused commits before starting this exercise. Full logs and isolated fixture data are in `/private/tmp/dockhand-followup-20260914/`.

## Candidate discovery

Repology search for `herbygillot@github` was attempted through the web tool and its API. The live site was unavailable (including DNS failure locally). Two saved Repology project-list pages in Downloads contain exactly that maintainer, MacPorts, and outdated filter. They were used only as leads; current upstream versions were checked independently.

- git-toolbelt is 1.9.3 locally and [upstream v1.12.0](https://github.com/nvie/git-toolbelt/releases/tag/v1.12.0). No competing open PR was found. Automatic preparation safely rejected its explicitly named single-distfile checksums. A manual update was prepared in the linked worktree `git-toolbelt-tree`, on `dockhand/exercise/git-toolbelt-1.12.0`, leaving the user's primary checkout on master.
- zix was another saved-list candidate. Its preview safely rejected unsupported fetch customization. No branch was created.
- timer, chrome-cli, bashunit, and dstask were already at their current upstream release. chrome-cli was still suitable for exercising the Xcode profile.

## Completed live scenarios

- `setup --xcode ~/Downloads/xcode_archives` selected Xcode 26.6 and successfully provisioned `dockhand-xcode-tahoe`. No archive download or new user input was needed.
- `verify chrome-cli --branch master --trace` selected the Xcode profile automatically, ran xcodebuild, passed verification, and released its VM. Job `job_PVLQIPBW27Q47TB5WF4CFX6SXV`, attempt `attempt_KHMPD2VSWDFMR4JLAYTAVZCD5W`.
- Two local, dependency-free fixture ports were authored for predictable declared tests. They used an isolated database and repository, with capacity one. Their build artifact intentionally reports text unrelated to the Portfile version; no executable-version equality was required.
- Probe A reached `port -d test`. Probe B was accepted but waited for admission. Canceling B completed without a provider run or VM: job `job_W6M3GJSGXSY7ETBYXK72YMAVIY`.
- A second waiter attached to A while the original driver was still running. The original driver was killed with SIGKILL. The surviving waiter completed the same single attempt, including its declared test, and released the VM: job `job_ZJJRP37GYH5XJG2HVFWNDHBEIR`, attempt `attempt_4NJIJFZPJ4PC3WZCWE4LAAWJC2`.
- Probe B was retried and canceled during its declared test. Both job and attempt became canceled, the VM was released, `cancel --wait` returned zero, and the attached verification returned 130: job `job_CIMGKXNYMEJ5P277MG2WHJZGAZ`.
- The original croc PR #34676 had already merged at 22:15:21 UTC. Repeating publication safely refused the merged PR. Lost-response and competing-publication fault cases were exercised through existing integration tests rather than introducing faults into a public PR; they passed.

The first orchestration script expected the literal word “capacity” in CLI output. The CLI instead said “waiting for provider admission.” That was a harness assumption, not a Dockhand failure; the remaining steps were driven from recorded state and direct CLI commands. An attempted capacity override from one to two was rejected because the existing fixture pool's configuration is durable; the exercise continued with one.

## Corrections found

Standalone verification had no recorded contribution base and therefore required a fully parseable ports-tree index. The manual git-toolbelt update failed on unrelated, pre-existing cabal and ghc Portfiles. Standalone indexes now permit unrelated omissions and are cached separately from indexes used to validate known changes. The Tart staging path explicitly requires the selected target to be indexed in its expected directory. Known-change coverage checks remain intact. Native and provider regressions cover partial-index isolation, a missing selected target, and a target indexed under the wrong directory. Test index fixtures now contain actual target metadata rather than deliberately incomplete records.

The failure also revealed that reconciliation replaced its useful staging diagnostic with a generic partial-provisioning message. Workflow now carries the prior error into that terminal status, while retaining normal resource cleanup. A regression covers this failure and cleanup together.

The standalone-index change passed the full Go test suite and vet. The diagnostic change passed focused regressions and the complete workflow suite. Both fixes were committed separately as `29af353` and `d8b96b3`.

## Additional observations

Standalone indexing of roughly 41,700 entries takes several minutes for each new tree. Source staging currently happens after VM startup, and the per-profile index cache lock serialized the real-port runs while their guests occupied capacity. These costs and the long stale-looking “queued” display are recorded in the roadmap rather than hidden by further optimization during this exercise.

The manual contribution first passed in job `job_MRSVFHAWPUGD2XVCIA4Y3OLM72`, attempt `attempt_DNXZ5UJL72NPTBT7UNCDKFLX56`. A commit-only amendment changed its commit ID without changing its tree; publication preview reused that exact attempt. A second amendment corrected “every day command line usage” to “everyday command line usage.” Publication then correctly rejected the stale result with `verify the committed contribution before publishing`. The script expected the noun “verification,” so that assertion was corrected by inspecting the result and continuing through the CLI; the product behavior was correct.

A modified working-tree fixture was also verified with an intentional test error. It captured exactly one tracked modification and rejected the earlier passing tree's evidence. Job `job_IKIQHTFW5Z33FX3X2ODBEC4YQR` failed in attempt `attempt_KVIDN5ZQK72FXIDXNAHHLQICBZ`, recording lint/build passed and test failed, attributed to target `dockhand-probe-a`, phase `test`. Garbage collection scoped to the isolated fixture database released its retained VM and pruned fixture diagnostics after full logs were captured separately. All fixture jobs are terminal and all fixture resources released. Integrity checks passed for both the fixture and normal databases.

Guest-agent version assumptions, saved-credential recovery, and older-schema status guidance remain follow-ups. No executable-version comparison was added to port verification. The new principle is documented explicitly in `principles.md`.

## Final result

The amended contribution passed in job `job_GCWIZVX6TAEMM5PUN2GGGBYELD`, attempt `attempt_TVFKSMHFPAHKCBDYIFZSCTXE45`, against tree `b064121fd48e8de19d9103799765f659e6cad179`. This is a different tree and attempt from the first verification. Lint, build, and installation passed; git-toolbelt declares no test phase. Its runtime dependencies were installed from available binary archives. The VM was released.

Publication preview selected the new attempt. Dockhand then published [MacPorts PR #34677: git-toolbelt: update to 1.12.0](https://github.com/macports/macports-ports/pull/34677) in job `job_HOKGUWH5LXFQLI6COAPZJLNYW3`. Immediately repeating publication completed job `job_WIF6VEZRCTOQU6SOE5BH6AP7GL` and confirmed the same PR without another branch push or PR-creation step in its driver output. An independent GitHub query found exactly one PR for this branch and confirmed its single commit, `8a4b789591e47b097fadad3a45220e2c6b552fc3`, changes only `devel/git-toolbelt/Portfile` (five insertions and five deletions).

Both publication invocations used the working GitHub CLI credential through a private child-process `GH_TOKEN`, as in the croc exercise. Saved credentials were not modified. The clean temporary contribution worktree was removed after publication; its local branch and PR remain. The primary ports checkout stayed on master, and its pre-existing untracked port directories were preserved. The new Xcode image and golden image remain available for normal use.

This exercise completed full-Xcode provisioning and verification, declared-test success and failure, working-tree capture, capacity waiting, queued and running cancellation, competing waiters, SIGKILL recovery without duplicate attempts, evidence reuse across commit rewrites, evidence invalidation after corrective edits, re-verification, publication, repeated publication, and fixture cleanup. Public-PR fault injection was covered by integration tests; it was not simulated against GitHub itself.
