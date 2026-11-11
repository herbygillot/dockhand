# Asking before dockhand evaluates someone else's Portfile

A feature for v0.9.0, [#162](https://github.com/herbygillot/dockhand/issues/162), placed at the person's word ("Each head, v0.9.0", 2026-11-11 00:12Z, in the architecture thread). The issue says where it stands; this file is the design, architecture's of 2026-11-10, read at main 9a0214ab. It answers the second finding of Codex's blind-spots review at that commit, that review crosses the host's trust boundary before any isolated build, on which the person chose "Ask first" (2026-11-10 23:02Z, in the blind-spots thread): review asks before it evaluates another person's Portfile, a script must pass a flag, and without the flag a script gets a text-only review. October's trust rule said whose Portfiles were being evaluated. This makes it a question.

## In short

- **One question, asked before the first evaluation**, with No as the default and the Portfile diff one key away.
- **The answer names the author's commit, not only the pull request.** A new push from the author asks again.
- **`--evaluate` is the script flag.** `--yes` does not answer this question.
- **A text-only review runs the rules that read text and says what it left out.**
- **Nothing unattended asks.** `serve` and a check's driver evaluate another person's commit only where the answer is already kept.
- **One door** replaces the ten places that print today's notice.

## What the code does today

A Portfile is Tcl that MacPorts runs as the person, on the Mac that holds the GitHub login. Today dockhand prints `Evaluating @alice's Portfiles on this Mac, for #35174` once and goes on (`engine/trust.go`). The notice is called by hand at ten places. Two evaluations get past it:

- **Review evaluates the pull request's head before it prints the notice.** `ruleCommits` builds the head's port index, which runs `portindex` over the changed Portfiles (`engine/review.go:219`, `engine/tidy.go:1176`, `macports/treequery/reader.go:230`). The notice is four lines later (`review.go:223`). v0.7.0 has the same order, so it is not a regression.
- **`serve` can evaluate another person's Portfile unattended.** It drives queued checks a person asked for (`checks/recovery.go:21`), an adopted branch's included, and the check's hooks evaluate the branch's Portfiles in the driving process (`engine/checkservice.go:64`, `:73`). `trust.go` says "Nothing unattended evaluates an unmerged Portfile another wrote". Read, not run.

## The question

Asked on a terminal, before anything of the author's is evaluated, when the pull request's author isn't the person and no answer is kept for the commit:

```
#35174 is @alice's. A full review evaluates its Portfiles with MacPorts on this
Mac, as you. A Portfile is code: it runs with your files and your GitHub login
in reach.
  devel/foo/Portfile                      +12 -3
  _resources/port1.0/group/foo-1.0.tcl    +4 -0
? Evaluate them: y yes · d show the diff first · n review the text only  [n]
```

- The files listed are the changed Portfiles and PortGroups, since a changed PortGroup is the author's code too.
- `d` shows their diff in the pager and asks again.
- Return alone is No. No is not kept: the next review asks again.
- Where the author couldn't be read it says `#35174's author's`, as the notice does today.
- A pull request of the person's own asks nothing, as today.

## Where the answer is kept

**Per pull request and author's commit.** A Yes is recorded as: repository, number, the author's head commit, and when. The review record already carries the same three (`model.Review`, `model/history.go:505`), so this is one more small record of the same shape.

**Why each head, where the first card said once per pull request.** The author can push after the Yes. Once per pull request would then run code the person has never been shown, which is the case the question exists for. Asking again costs one more question per new head, and that question is narrower:

```
#35174 has a new head, 4f2a1c9. You allowed a1b2c3d on 2026-11-10.
  devel/foo/Portfile                      +2 -1   (since a1b2c3d)
? Evaluate it: y yes · d show what changed · n review the text only  [n]
```

**The person's own commits don't ask.** On an adopted branch the answer is for the author's commit the branch was adopted at or last moved to (`RefreshAdopted`, `engine/branches.go:1030`). Commits the person adds above it are theirs. The branch's record has to keep the author's commit apart from `Pushed`, which becomes the person's push once they submit to the pull request.

## The flag

- `--evaluate` on `review <pr>` and on `adopt --pr <n>`: answers Yes for the head fetched, records it, and asks nothing.
- `--evaluate=false` on `review <pr>`: a text-only review with no question, for a terminal too.
- `--yes` never answers it. `--yes` means "go on with what the plan shows", and this is a different kind of consent.
- The name uses the usage docs' own verb. `--trust` reads as trust in the person and outlives the commit; `--run-portfiles` is longer and says the same.

## A text-only review

What it runs, all from Git objects and the base's index, which is MacPorts' own master, already merged:

- The commit rules (subject, body, trailers).
- The Portfile rules that compare text: `CheckPortfiles` and `CheckNotices` (`engine/tidy.go:1242`).
- The ports and dependents, as the base's index names them.
- What the last review of this pull request found that is now resolved.

What it leaves out, and says:

- The two rules that evaluate: restated values (`CheckRestated`) and path-style dependencies (`CheckDependencies`).
- A port the pull request adds as a subport, which only the head's index names. The rule that a subject names its port isn't run for a commit whose directory changed, since it would misfire.
- The comparison with upstream's change.
- The build.

A rule that needs the head evaluated is not run, never half-run. The report says so in every form:

- Displayed and `--markdown`: a first line, `Text-only review: #35174's Portfiles were not evaluated. Not run: restated values, path-style dependencies, the upstream comparison.`
- Posted text carries the same line, so the author and other reviewers know what this review did not look at.
- `--json`: `"evaluated": false` and `"left_out": [...]`.

## Every place that evaluates another person's Portfiles

| Entry point | Today | With the question |
| --- | --- | --- |
| `review <pr>` on a terminal | Evaluates, then says whose | Asks first. No gives a text-only review |
| `review <pr>` in a script | Evaluates, says whose | Text-only unless `--evaluate` |
| `review <pr> --markdown` | Evaluates | The same two rows: `--markdown` changes the output, not the evaluation |
| `review <pr> --check` | Adopts, plans on the host, builds in Tart | Needs a Yes: planning a check evaluates on the host. A script without `--evaluate` is refused before anything is adopted. There is no text-only check |
| `adopt --pr <n>` | Evaluates nothing | Asks nothing. `--evaluate` records the answer for scripts |
| `check`, `tidy`, `submit`, `diff`, `update` on an adopted branch | Evaluates, says whose | A terminal asks if the author's commit isn't allowed. A script is refused, naming `dockhand adopt --pr <n> --evaluate` |
| `status` | Reads without evaluating (`collect` false) | Unchanged. Never asks. Says "not evaluated" where it would have shown evaluated facts |
| `serve`, and `wait` or `watch` driving a queued check | The hooks evaluate | Never ask. They drive a run only where its author's commit is allowed, which holds for every run queued after this lands. One that isn't stays queued and `status` says why |
| A local check of an adopted branch | Refused (`checks/queue.go:90`) | Unchanged. A Yes doesn't lift it: that build would run as root |

## How it is built

- **The engine never asks.** The command asks through `term` and records the answer; the engine reads the record. A refusal from the engine names the command that gives the answer.
- **One door, not ten call sites.** `sayWhose` goes. The check sits where a tree is staged for evaluation (`workspaces.Acquire`, seven callers in the port reader), and a test like `outward`'s fails if evaluation is reached another way. How the door learns whose tree it has, a mark on the context set where a command picks its branch or pull request, or a mark on the source, is implementation's to choose. Sources are built at 43 places, so the context is the likelier.
- **This is consent, not a sandbox.** `docs/principles.md`'s limit, "Harden the host against a hostile Portfile", stays as it is: a Tart guest is the boundary, and planning still evaluates on the host. Evaluating another person's Portfile inside the guest would be the real fix and is a larger feature, not designed here.

## Tests

- `command/review/reviewpr_test.go:35` expects a script review to evaluate and print the notice. It becomes: text-only without the flag, full with it.
- New: a second head asks again; the person's own commit on an adopted branch doesn't; `serve` leaves an unallowed run queued; `--yes` doesn't answer.
- Acceptance row E9 greps for the notice (`tools/acceptance/rows/E9.sh:23`) and changes with it.

## Size and placement

About a third of a batch, inferred from the list above. It needs none of v0.9.0's core items, and comes after the package moves so it lands once in the new layout.

## Not checked

- The `serve` route by running it. Read from `checks/recovery.go:21` and `engine/checkservice.go:64`.
- How the interpreter session quotes its arguments. A text-only review still hands two version strings from the author's text to MacPorts' `vercmp` (`macports/eval/versions.go:112`). It evaluates no Portfile, but the strings are the author's.
- Every caller of the port reader outside the engine, such as `portedit` and `survey`, for trees that could be another person's.

## Usage docs

- `docs/usage/review.md`, "Decide whether to evaluate the contributor's code": rewritten around the question, `--evaluate`, what a text-only review covers, and that `--check` needs a Yes. The script paragraph above it gains one sentence on text-only.
- The usage page for `adopt`: `--evaluate`.
- `docs/decisions.md`: the entry recorded with this design, adopted and not yet built, moves to built in the implementing commit.
- `docs/architecture.md`: names the one door.
