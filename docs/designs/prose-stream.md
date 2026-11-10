# Standard output wraps only dockhand's own prose: `Out` raw, `Prose` wrapped

A core item for v0.9.0, [#94](https://github.com/herbygillot/dockhand/issues/94), placed at the person's word ("Agreed with the wrapping write-up", 2026-11-06 03:16Z, in the architecture thread). The issue says where it stands; this file is the design, from architecture's review of dcef3282 of 2026-11-06, read at main f769954e, after the person's ask in the user-feedback thread (02:53Z): "Can we ensure that we won't break the Tcl code when we column wrap in general?" The four writers below are v0.8.0's bug, [#93](https://github.com/herbygillot/dockhand/issues/93), fixed the way the six opted-out commands are; this item is what makes the opt-out unnecessary.

Revised 2026-11-10, at the person's word ("Revise all", 23:24Z, in the architecture thread), after Codex's design read of this file at main 9a0214ab and architecture's check of it: the shape is unchanged; the test contract is corrected, since a command that mixes sentences with a payload can't print the same bytes at every width; a stage's summary, serve's stamp and a command run inside the console are designed; and three routes found while checking are this item's cases ("Still reaching it", below).

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

## Still reaching it, at main 9a0214ab

#93 gave the four writers above their `Unwrapped()` calls, twelve call sites in all. Checking Codex's read found that the call doesn't always reach the wrapper. Each was shown by a scratch test of `term`; the command paths were read, not run.

1. **On a stage's streams, `Unwrapped()` does nothing.** It looks for a `*switchWriter` in `Out` (wrap.go:143), and `Staged` has put a `*stageWriter` there (levels.go:44). `ship` hands staged streams to tidy's stage (ship.go:668), so tidy's "Review diff [d]", opened from `ship`, wraps.
2. **A command run from the console keeps the console's wrapper.** The console passes its begun streams into the command (console.go:387), `Begin` wraps them a second time (root.go:105), and the command's `Unwrapped()` turns off only the new one. Logs, tidy's diff and submit's preview, opened from the console, still pass the first.
3. **`Plain()` misses the mark writer.** It expects the mark writer directly under the switch (display.go:36), and `Begin` puts the wrapper between them (streams.go:47 to :52). `console --plain` on a terminal leaves its marks coloured.

None is a regression: v0.7.0 wrapped all of it. They stay with this item, which removes the call that fails to reach.

Two corrections to the list above. The diff at author/edit.go:147 is `revbump --plan`'s; `edit` prints none. And "tables are exempt" is not safety for code: a line with two spaces between words is not wrapped, but on a terminal it is cut at the width with "…" (wrap.go:63, :106), and a Portfile's `long_description    ...` is such a line.

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

What the mechanics above left out:

- **A stage's own lines go through its Prose.** `Stage.Done` writes to
  the writer `Staged` captured (levels.go:80). With the holding writer
  under both, that writer is raw, and a stage's "✓" line would lose its
  wrap and its colour. `Done`, `Say` and `Drop` write through the stage's
  Prose.
- **serve's stamp sits beneath both.** serve replaces `Out` with a
  stamping writer where output isn't a terminal (serve.go:102). A Prose
  built before that would write around the stamp, and "serve: stopped"
  and "serve: waiting" would lose theirs. The stamp is installed where
  the `--json` switch is, under `Out`, by a method of `Streams`, and no
  command assigns to `Out`.
- **`Begin` on streams already begun adds no second wrapper.** The
  console's commands get the console's raw `Out` and one Prose over it. With `Out` raw by construction, a command's payload is
  raw whatever started it.
- **`Plain()` reaches the mark writer through Prose**, which it misses
  today.
- **Every test of what `Out` is gets read again:** `Ask`'s flush
  (streams.go:144), `Held` (levels.go:52), `colourMarks` (display.go:48)
  and `underlying` (streams.go:166), which doesn't know serve's stamp.

Churn: about 200 `streams.Out` sites in internal/command (205 at main
5708f83e; setup, tidy, submit, ship and clean hold the most), renamed to
`Prose`, with the audit deciding which stay `Out`: the four above and the
six opted out. The audit is mostly in the 20 or so `out := streams.Out`
aliases (create.go:68, author.go:471, edit.go:120, tidyrun/stage.go:23),
whose writes go through `out` and through helpers' arguments and mix
sentences with payload. It is one change to the output pipeline with an
audited migration, not a rename alone. One commit, about a fifth of a
batch with the audit and the tests, inferred.

Part 5 item 4 stands: prose still wraps at 100 off a terminal, for the
pipe field testing reads. The feedback thread's second option, no wrapping
off a terminal unless COLUMNS is set, isn't needed under this and isn't
proposed; it would reverse that decision for nothing the inversion doesn't
give.

## Tests

- term: `TestOutIsNeverWrapped`: the 131-column modeline and a 300-column
  `reinplace` line written to Out come back byte for byte, on a stand-in
  terminal of 80 columns too; the same lines to Prose wrap.
- Two cmdtest helpers, from the feedback thread's idea. Both run a
  command with COLUMNS=20 and COLUMNS=100000, through the stands'
  environment (stands.go:107).
  - `SameAtAnyWidth(t, args)` asserts identical standard output. It is
    for a command that prints only a document: `usage configuration`
    (fenced blocks), `completion fish`, and `review --markdown`.
  - `PayloadAtAnyWidth(t, payload, args)` asserts that the payload's
    bytes are in the output, whole, at both widths, and that a sentence
    of dockhand's own wraps at 20. It is for a command that mixes the
    two: `create --plan` (its stand-in project), `update --plan` and
    `revbump --plan` on a fixture whose Portfile has a 130-column line,
    `submit --plan`'s description, and `diff`, which writes a summary
    line before its patch (understand.go:64). Whole-output equality can't
    hold for these: `create --plan` and `update --plan` already differ at
    the two widths today, where their sentences wrap.
- The routes a person opens by a key: tidy's "d" and submit's "p", each
  on its own, from `ship`'s stage, and from the console.
- A payload line that reads as a table, `long_description    ...` wider
  than the terminal, comes back whole on a stand-in terminal, and a
  payload line that begins with a mark is not coloured. `AsTerminal`
  doesn't turn colours on (display.go:44), so the colour half is a test
  of the mark writer itself: Prose passes through it and `Out` never
  does.
- A stage: held, `Say`, `Drop`, `Done`, a question's flush, `-v`,
  `--json` silencing both writers, raw and prose written in turn and
  read back in order, and `Cut` on Prose only.
- serve off a terminal: a sentence and a log line both carry the stamp.
- `Plain()`: no mark is coloured.
- dcef3282's test stays, and so do #93's, which today are `Contains`
  checks at one width. The display and wrap tests keep covering Prose.

## Size and placement

About a fifth of a batch with the audit and the tests, inferred: one mechanical commit over forty or so files, after v0.8.0's candidate is cut, which is why it isn't in v0.8.0. Usage docs: none; no command or flag changes. v0.9.0's release notes gain a "for developers" line.
