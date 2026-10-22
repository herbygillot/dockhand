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
