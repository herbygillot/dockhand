# Host state the version doesn't read

The roadmap's v0.5.0 part 3, item 2, taken while v0.4.0's tag waits.
`bump librecad` refused with "version probe is inconclusive: modeled
context depends on host state: filesystem state outside the captured
ports tree (Portfile:5)", and so had cpi (#35403), though each version is
a plain `github.setup` line. `PortGroup qmake5 1.0` reads the host as it
loads. Its `platform macosx` block reads `use_xcode`, whose default Base
computes by asking whether /usr/lib/libxcselect.dylib is there, and it asks
whether `${configure.developer_dir}/SDKs` is there, to set `use_xcode
yes`. Every context modeled for another Mac counted that against the
version and the archives.

- `buildDefaultReadBenign` (observe/host_access.go) explains a host read
  Base makes computing an option's default, where every option on the way
  to the read is build-only (`use_xcode`, and the toolchain's options).
  Neither the Portfile nor a PortGroup on the read's stack may read any of
  them outside a build-only position (`classifyReads`), and the Portfile
  may not read them at all where the walk can't see, as inside an
  expression's braces (`$name`, `[option name]`, `[tbool name]`).
  configure.cc's default reads configure.compiler's, so each default of the
  chain is held to it: feh's reads, which reach `${configure.cc}`, stay
  judged as before.
- A host read inside a `platform` block of a PortGroup is benign where
  the whole body is (`benignBodies`), as qmake5's `if {...} {use_xcode
  yes}` is. `use_xcode` is a benign sink, since it chooses the build's
  tools, not an archive.
- An overlay links the tree's PortGroups, so the PortGroup directory is
  resolved through links before a frame's file is matched against it. A
  candidate's context was inconclusive where the baseline's had been
  explained.
- A read stays inconclusive where it reaches the version or an archive: a
  PortGroup branch that sets master sites, a Portfile version that reads
  `use_xcode`, and a version line that reads the host itself.
  `TestHostStateTheVersionAndArchivesDontReadIsExplained` covers each.
- `-vv` now says which rule explained each host read.

Replayed in a scratch tree, before and after:
- `update LibreCAD --plan` refused and now plans 2.2.1.6's `github.setup`
  and checksums.
- `update cpi 2.2.3 --plan` refused ("baseline {OS:darwin Version:25
  Architecture:arm64}: ... depends on host state ... (Portfile:5)") and now
  plans its `github.setup` and checksums.
- Layer 1's set's `update --plan`s are the same.

A survey of the 666 ports step 2's whole-tree run showed refusing for host
state: 97 moved from unknown to input-found. Among them are LibreCAD, cpi,
qt5 and the Qt 5 ports, the PyQt5 ports, qscintilla, texstudio, and
wkhtmltopdf. The other 569 read the host in their own Portfiles or in
ways these rules don't explain, as most of the Qt 5 family's Portfile
lines do (Portfile:759, 740, 721 ...). Each port that moved does so
through these two rules alone, and no host read they admit reaches a
version or archive. No whole-tree pair is needed: what the observer
records is unchanged, and only its judging is.

v0.4.0's known issues gain a row, since it's dozens of ports. Its
workaround is the hand fix: edit the version line, then `dockhand
checksums <port>`. rc5's `checksums cpi` runs, since it evaluates this
Mac's context.
