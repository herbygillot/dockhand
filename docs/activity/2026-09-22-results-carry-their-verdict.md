# 2026-09-22: results carry their verdict

## What the review measured

The 2026-09-21 architecture review found `cli` with the highest fan-out
in the tree, twelve imports reaching past `app`, most of them for
constants: `assess` compared each port's outcome against `portedit`'s
outcome names to decide the exit status and the hint, `outdated` compared
against `upstream.Unknown`, and the bump commands asked
`portedit.ErrUnsupported` whether to print the adopt hint. The CLI was
re-deriving "needs attention" from the internals of a result instead of
asking the result.

## The change

`assess.Result` tallies its ports by outcome, `Tally`, and the tally says
whether the assessment is incomplete; `assess.Port` words its own
headline. `outdated.Result` says whether any observation is unknown and
`outdated.Port` words its assessment. `app.IsUnsupported` classifies a
preparation error as the editor not handling the Portfile's shape, the
case a person finishes by hand and adopts. The commands print the same
words and return the same exit statuses as before; the JSON is unchanged,
since the verdicts are methods, not fields.

`cli` no longer imports `macports/portedit` or `upstream`, and its
package-dependency test no longer allows them. The remaining reach past
`app` is `macports`, `git`, `record`, `verify`, `publish`, and `github`,
which carry the request and evidence types the commands construct and
render.

## Left

The review's next organization items stay in the roadmap's order: the
evidence projection in `workflow/view` that status, the summary, and the
pull request body would share, and the command pipeline over the `r.build`
prologues once multi-target contributions settle the command surface.
