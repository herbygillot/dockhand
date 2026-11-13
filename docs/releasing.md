# Releasing dockhand

How a release goes from its candidate to its GitHub Release, and what follows it. It is written so that someone who was not there for the last release can run this one. What decides a candidate, and what holds one, is in [development](development.md) ("How work gets in" and "Release candidates"); how the acceptance harness works is in its [README](../tools/acceptance/README.md). This page gives the order and the commands, and doesn't repeat those two.

`vX.Y.Z` is the release and `rcN` its candidate throughout.

## Rules that bind every step

- The person alone types a tag push, a pull request into MacPorts' or dockhand's own repository, and every sudo command. Pull requests into the test account's fork are allowed standing.
- The test host's session never opens pull requests or issues on dockhand's repository. What it finds is reported, and the fix lands from a development checkout.
- Commits follow [AGENTS.md](../AGENTS.md): the person as author, `Assisted-By` for an agent, never `Co-Authored-By` for one.
- Nothing that reaches GitHub names a person with a bare `@`.
- Harm, or a regression since the last release, makes another candidate. An ordinary bug becomes an issue in the next release's milestone and a known-issues line.
- The release's notes carry new features and known issues only, and make no timing claims; figures that don't depend on hardware are fine.
- Tokens are never printed.

## Where things are

| Thing | Place |
| --- | --- |
| The harness and its rows | `tools/acceptance/`, a row is `rows/<ID>.sh` and its header names its stages |
| Results | `$ACCEPT_STATE/results/<candidate>/<row>.json` on the test host |
| The release's branch | `release/vX.Y`, made at the first candidate once main takes the next release's work; fixes land on main first and are picked onto it |
| The release's notes | `docs/releases/vX.Y.Z.md`, added to as each change a person sees lands |

## 1. Acceptance of a candidate

1. **Quick stage first**, on the test host, on the candidate: `tools/acceptance/quick.sh`. The full stage starts only when it has no fail and no blocker, and doesn't run again a row that passed there unless `tools/acceptance/full-reruns.txt` names it.
2. **Full stage**, as the test user, detached so that a closed session doesn't end it:
   ```sh
   nohup tools/acceptance/full.sh --candidate vX.Y.Z-rcN > ~/full-rcN.log 2>&1 &
   tools/acceptance/full.sh --candidate vX.Y.Z-rcN --rows "D-I6 G14"   # named rows only
   ```
   A row refuses a binary whose `--version` doesn't name the candidate, so the candidate is installed first (row A1).
3. **Checkpoints.** A run that prints `WAITING: <row>: <what>` needs a person at the host. They do the step, then `tools/acceptance/resume.sh done` (or `skip`, or `fail`). `tools/acceptance/pause.sh` holds the run between rows.
4. **The sitting.** The rows that need the person at the host (sudo, a reboot, a login, a new GitHub login, and S1, the walk a new user takes) are gathered into one sitting, its sudo steps in one window. Each step is said to them in one line, and the run waits for their "done".
5. **Judgments.** `tools/acceptance/judge.sh` lists the claims a person must judge and takes `pass` or `fail` for each, from the row's `out.log`. An unsure one, or one that touches harm, goes to the person.

### Reading a result

- `pass`, `refused well` and `known` are fine.
- `not run`, `not exercised` and every `blocked: …` are neither a pass nor a failure: the cause is fixed and the row runs again.
- `fail`: the row's log says whether dockhand or the harness is at fault. A harness fault is fixed in `tools/acceptance` on main, and the row runs again at that commit, which the result records.
- **A blocker** is a row that broke one of the harm invariants, H1 to H9, whatever the row said of itself. It is harm until evidence shows the harness made it.
- The test account's GitHub allowance is never spent to zero during a run; each result's `github_used_after` says where it stands.

## 2. The verdict

A release goes out when every row of the full list has run on the last candidate, or on an earlier one whose code for that row didn't change; S1 passed with the person; and no harm and no regression since the last release is open. The write-up gives the counts by result, each row that didn't pass with its cause and its issue, the harness commits used, and one sentence: pass under the bar, with the commit to tag, or the harm that prevents it. The person reads it and says go.

Where harm is found: a failing test, the fix on main, the pick onto the release's branch, the next candidate's tag at the person's word when the test host is free, then the quick stage and the rows the fix touches.

## 3. Tag and release

1. On the passing commit, the notes' `acceptance:` comment line takes the result, and "What's new" and "Known issues" are finished and read once more against the two rules above. `make gate` and `make sweep` pass. The same notes land on main.
2. Open issues still in the release's milestone move to the next one, unless they are harm: `gh issue list --milestone vX.Y.Z --state open`.
3. The person types:
   ```sh
   git tag -a vX.Y.Z -m "dockhand vX.Y.Z" <commit>
   git push origin vX.Y.Z
   ```
4. CI on the tag is green (`gh run list --branch vX.Y.Z`). A red run stops the release, with its reason said.
5. Every version gets a GitHub Release. Its body is the release's notes, with the README's link pinned to the tag's commit. The person sees it first:
   ```sh
   gh release create vX.Y.Z --title "dockhand vX.Y.Z" --notes-file <body.md> --verify-tag
   ```
6. dockhand's own port is updated through dockhand, and its pull request waits for the person's word.

## 4. After the release

1. Those who reported something the release fixes are told, on their issue, with the release's link.
2. The round over issues with no milestone, as [development](development.md) sets it out: `gh issue list --search "no:milestone is:open"`, a recommendation for each, and the person decides.
3. Upgrades of the test host that a release must not meet halfway, such as Tart's, happen now.
4. Tests that need the released build on the test host, or a rule only the person can add there, run now, and the issues that wait on them close on their result.
