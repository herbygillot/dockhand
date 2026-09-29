# 2026-09-29: a failure's likely cause, from its log

The person decided D10 on 2026-09-29, from the beekeeper-studio run's finding 2. beekeeper-studio failed on macOS 12 and 13 with "`make` failed with exit code: 2; Failed to build beekeeper-studio", MacPorts' own `Error:` lines, which the Tart guest takes as a failure's summary. The cause, clang's "fatal error: 'source_location' file not found" as node-gyp rebuilt sqlanywhere, was only in the log.

Now a failure's summary adds the first compiler error in its log, marked as read from it: "`make` failed with exit code: 2 · from its log: ../src/h/sqlany_utils.h:14:10: fatal error: 'source_location' file not found".

- **`buildlog`**, a package of its own, holds the reading (`First`). It takes a line in the format clang documents for its diagnostics, which GCC and swiftc share: "file:line:column: error: message", or "fatal error:", the column optional. Warnings and notes aren't errors, nor is make's "Error 1" or MacPorts' "Error:". The first one is taken, since in a C or C++ build the rest follow from it. More readings can join it, best effort, as the person expects them to over time.
- **On this Mac, for every provider.** The engine reads the log when it records a failed result (`withCause`), since each provider has brought the target's log here by then, rather than the Tart guest's script reading it. So GitHub's runners and a command provider's builds are read alike, and the reading is Go, tested. A GitHub runner's log has each line stamped with a time, so nothing matches it yet; that reading can come when it's wanted.
- **MacPorts' words stay first.** The reading is added beside them, and a log with no compiler error, or one that can't be read, adds nothing.

The engine imports `buildlog`, named in its boundary. The guide's table of results, and the architecture note, say so.

Tests: `TestTheFirstCompilerErrorIsTheLikelyCause`, in `buildlog`, over beekeeper-studio's lines, GCC's without a column, a line past the scanner's first buffer, and lines that aren't causes; `TestAFailuresDetailSaysWhatItsLogShows`, through a check's drive. Nine mutations each fail a test.
