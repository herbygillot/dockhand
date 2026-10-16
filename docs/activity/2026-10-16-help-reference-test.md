# A test that every command the help and docs name exists

Part 1's hygiene, second step. `TestEveryNamedCommandExists`
(internal/command/helpreference_test.go) reads each "dockhand <command>
[--flag]" that the help, every flag's usage, the README, docs/*.md, and the
string literals of dockhand's own source (the `Next:` lines and refusals)
name, and refuses one whose command or flag doesn't exist. v0.3.0's docs
taught `auth login` and `providers setup tart` after both had moved; this
is what keeps the next rename from leaving them behind.

- **Code and prose read differently.** In Markdown, only fenced blocks and
  inline code are read, and every "dockhand <word>" there is a command. In
  prose, a help or a message, "what dockhand runs with" is a sentence: a
  word that names no command is a missing one only where a command is
  offered, after a colon, a parenthesis, "run", "then", "try", "use",
  "see", or "Next", and seven words that begin a sentence there ("(dockhand
  built jq …)") are listed. A flag is checked wherever the word before it
  names a command, so "dockhand status --everything" is refused anywhere.
- **The root's tree** comes from `inspectRoot`, a test seam Run calls with
  the tree before it executes, beside the package's other seams.
- **Left out:** the roadmap, and four design records that name commands
  proposed or retired: design-v3.md (whose sketches name `info`, `db`,
  `logs --errors`, and `setup prefix`, never built, and whose v2 table names
  old spellings on purpose), directions.md (`doctor`, `why`),
  build-platforms.md and bump-coverage.md (v2's `verify` and `publish`). The
  hygiene item had design-v3.md read apart from its table; its first-run
  sketch is fixed (ad9f8264), but its other unbuilt names would each need
  an exception, so the file is left out whole. The README's v0.3.1 known
  issue naming `dockhand auth status` is the one exception kept, as v0.3.1
  has it.
- It found nothing beyond what ad9f8264 fixed. `TestReferenceProblems`
  shows it refusing `dockhand providers`, an unknown flag, an unknown
  shorthand, and "run dockhand auth login", and reading the sentences above
  as sentences.
