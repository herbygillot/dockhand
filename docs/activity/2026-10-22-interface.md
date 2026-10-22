# Part 5: the interface on top

The roadmap's part 5, after part 4's verdict and split.

## R3-34: status's JSON gives the verdict

`status --json`'s branch had its latest and active checks, and left the
standing to be judged from them again, though `CheckStanding` exists on
the value the view reads. Each branch now has `check_standing`,
`branchstate.Standing`'s name (`Standing.String`): `not-checked`,
`checking`, `stopped`, `passed`, `incomplete`, `older`, `failed`, or
`attention`. Serve's state was already fields (`serve_state`) beside its
sentence.

## Item 1, §3's chaining

### tidy's message composer is commitmsg's

This is the code-organization review's finding 9. Its dead half
(`Compose`, `Rewrite`, portedit/message.go) was already gone.

- The live composer moved from engine/tidy.go into commitmsg, beside the
  rules it writes to: `Compose(subject, text, combined messages,
  generated)`, `Body`, and `SplitTrailers`.
- engine/body.go's PR description reads `SplitTrailers` from there too.
- create's subject is "<port>: new port", as Design v3 §8 and tidy's
  `derivedSubject` give it, where create wrote "…, version X".
