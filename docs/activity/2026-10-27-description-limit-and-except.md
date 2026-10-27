# The description fits GitHub's limit, and lists what --except left out

2026-10-27, the docs dig's I41, its first two parts (roadmap's "The pull
request's body fits GitHub's limit, and says what `--except` left out").

- **Trimmed under 65,536 characters** (macports/prdescription `Compose`,
  `Limit`, `Fits`): a description past GitHub's limit, which refuses it
  after the push so no pull request opens, is trimmed, the Tested on
  table's rows first ("| 508 more ports are left out to keep within
  GitHub's limit on a description |"), then the reports' run names (the
  author's `dockhand logs` has them), then the commits' table (the Commits
  tab lists every one), each as few as fit, by halving. One within it is
  as composed. prdescription may import internal/prose for the counts.
- **A merged description past it is refused before the push**
  (ApplySubmit): Compose's always fits, so only a part kept as a person's
  own can pass it; the refusal says to shorten it or `--rewrite-description`.
- **--except is recorded and listed**: schema 43 adds `edits.excepted`, and
  each revision bump's edit keeps the dependents `--except` left out of
  the same rebuild (UpdateRequest.Excepted, RevbumpLinked). The
  description lists them with the hand edits, a line a rebuild: "Left out
  of the rebuild for jq 1.8.1 with `--except`: **gojq**." `--except` takes
  no reason, so none is given; and a rebuild whose every dependent was
  excepted has no bump to keep them on, so it lists nothing.
- Tests: TestADescriptionPastGitHubsLimitIsTrimmedUnderIt,
  TestTheDependentsLeftOutAreListed, and the linked-ports test's edit.
