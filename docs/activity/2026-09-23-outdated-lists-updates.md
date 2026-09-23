# 2026-09-23: outdated lists what is out of date

`outdated` printed a line for every selected port, current or not. Asked
about a maintainer's forty ports, it answered with forty lines to find
the three updates in. It now lists only the ports with an update
available, the way `port outdated` and `brew outdated` answer.

The rest is counted, not dropped. After the list, one line on stderr
says what was left out, "Not listed: 12 current, 1 could not be checked;
--all lists them.", and a port that could not be checked still makes
the command exit with an error, whose message now points at `--all`
since the rows it names are not printed. `cli-design.md`'s rule that
unknown results stay visible holds; they are visible as a count. With
nothing to update and nothing unknown, the command says "No updates
available."

`-a`/`--all` restores the whole list, every port with its verdict and
the reason an unknown one failed, in text and JSON alike. The name
follows `status --all`, which also means "what is hidden by default".
`assess --all` means the entire ports tree instead; `outdated` shares
`assess`'s selection type but has never offered that, and the help for
`--not-maintainer` on `outdated` said it "goes with --all too", which
was already wrong and is gone.

JSON follows the same rule as text: without `--all` its `Ports` holds
only the updates. The filter is `outdated.Result.OutOfDate`, which
returns the report and a count of what it hid, so the command reads the
verdicts from the result rather than restating them.

The command's existing tests assert on current and unknown rows, so they
ask for `--all`; a new CLI test covers the default in text and JSON, and
a unit test the filter and its note.
