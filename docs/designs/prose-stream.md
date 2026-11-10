# Standard output wraps only dockhand's own prose: `Out` raw, `Prose` wrapped

A core item for v0.9.0, [#94](https://github.com/herbygillot/dockhand/issues/94), placed at the person's word ("Agreed with the wrapping write-up", 2026-11-06 03:16Z, in the architecture thread). The issue says where it stands; this file is the design, from architecture's review of dcef3282 of 2026-11-06, read at main f769954e, after the person's ask in the user-feedback thread (02:53Z): "Can we ensure that we won't break the Tcl code when we column wrap in general?" The four writers below are v0.8.0's bug, [#93](https://github.com/herbygillot/dockhand/issues/93), fixed the way the six opted-out commands are; this item is what makes the opt-out unnecessary.

## The fix: dcef3282 (#88)

- What it does: root.go makes cobra's completion command once the output
  is set and wraps each shell subcommand's `RunE` to call
  `streams.Unwrapped()` first; a test runs `completion fish|bash|zsh` on a
  stand-in terminal and asserts a line longer than 100 columns survives
  and "\ncompletions are run" is absent.
- Verdict: correct and minimal. `InitDefaultCompletionCmd` returns when a
  completion command exists, so cobra's own call in `ExecuteC` is a no-op
  after it (cobra v1.10.2, completions.go:750). The test is the right
  shape for one writer.
- Its limit: it is the sixth opt-out (`logs`, `serve`, `review
  --markdown`, `usage`, `diff`, now `completion`). The wrapper is on by
  default and each writer of code must remember to turn it off, so the
  answer to "can we ensure" is no, not with this structure.

## Where code reaches the wrapper today, unprotected (main f769954e)

1. `create --plan`: the whole Portfile, "Would write <dir>/Portfile:"
   (create.go:101) through `out := streams.Out` (:68). The modeline is
   131 columns, so its first line breaks every time, off a terminal too.
2. `update --plan` and `bump`'s plan: "Plan, nothing changed:" and the
   diff (author.go:605, `out := streams.Out` at :471); `revbump --plan`
   and `edit`'s diff (edit.go:146).
3. tidy's "Review diff [d]" (tidyrun/stage.go:48, `out := streams.Out`
   at :23): the staged diff.
4. submit's preview "[p]" (submit.go:135) and `--plan`'s description head
   (`writeDescription`, :220): Markdown, fenced code inside.

Not at risk: `--json` (Finish writes the envelope to the stream captured
before the wrapper, json.go:122); the files dockhand writes (Portfiles go
through the filesystem, never through streams); tables (a line with two
spaces between cells is exempt); the six opted-out commands.

## Measured: `term.Wrapped(line, 100)` over macports-ports 744df9d1

| | |
| --- | --- |
| Portfiles | 20,252 |
| first line (the modeline) wraps | 18,926 |
| code lines that wrap | 7,038, in 2,122 Portfiles |
| comment lines that wrap | 1,482 |

The modeline's second half, `vim:fenc=utf-8:...`, lands on a new line
without its `#`: a Tcl command. R-ChemmineOB's `reinplace
"s|@PKG_CPPFLAGS@|...` breaks inside its quoted string. A terminal
narrower than 100 columns breaks more. (The feedback thread's counts
differ a little: another tree state.)

## The guard: wrapping opt-in, by construction

`term.Streams` keeps `Out` as raw standard output, the `--json` switch
beneath it and nothing else, and gains `Prose`, the writer that wraps and
colours marks, built over `Out` by `Begin` and by `Staged`. Dockhand's own
sentences, tables, and views write to `Prose`; anything that comes from a
file, a tool, or a template (a Portfile, a diff, a log, a completion
script, a usage page, a Markdown body) writes to `Out`. `Unwrapped()` goes,
with no bridge.

Why this answers "ensure": the unmarked name is the safe one. A writer who
types `streams.Out` by habit gets raw output. Forgetting `Prose` costs a
long line; forgetting `Unwrapped()` today costs a broken Portfile. The
asymmetry flips to the harmless side, and no list of exceptions has to be
kept current.

Mechanics: `Begin` makes Out = switch(raw) and Prose = wrap(mark(Out));
`Staged` puts the stage's holding writer under both, so a held stage holds
both; `Plain()` reaches the mark writer through Prose; `underlying`,
`Beneath`, `OutTerminal`, and `Width` walk from Out as now. Writes to
Prose and Out interleave in order: the wrapper holds nothing past a line
(wrap.go:36-74). `Cut` stays with Prose.

Churn: about 250 `streams.Out` sites in internal/command (setup 36, tidy
25, submit 18, ship 16, clean 14, the rest smaller), a mechanical rename
to `Prose`, with the audit deciding which stay `Out`: the four above and
the six opted out. One commit, about a fifth of a batch with the audit and
the tests, inferred.

Part 5 item 4 stands: prose still wraps at 100 off a terminal, for the
pipe field testing reads. The feedback thread's second option, no wrapping
off a terminal unless COLUMNS is set, isn't needed under this and isn't
proposed; it would reverse that decision for nothing the inversion doesn't
give.

## Tests

- term: `TestOutIsNeverWrapped`: the 131-column modeline and a 300-column
  `reinplace` line written to Out come back byte for byte, on a stand-in
  terminal of 80 columns too; the same lines to Prose wrap.
- A cmdtest helper, the feedback thread's idea: `SameAtAnyWidth(t, args)`
  runs a command with COLUMNS=20 and COLUMNS=100000, through the stands'
  environment (stands.go:107), and asserts identical standard output.
  Applied to `create --plan` (its stand-in project), `update --plan` on a
  fixture whose Portfile has a 130-column line, `diff`, `usage
  configuration` (fenced blocks), `completion fish`, and `review
  --markdown`. dcef3282's test stays.
- The display and wrap tests keep covering Prose.

## Size and placement

About a fifth of a batch with the audit and the tests, inferred: one mechanical commit over forty or so files, after v0.8.0's candidate is cut, which is why it isn't in v0.8.0. Usage docs: none; no command or flag changes. v0.9.0's release notes gain a "for developers" line.
