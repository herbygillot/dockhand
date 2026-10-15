# A command that waits says on what (v0.4.0 part 1, c)

Field testing at c9ad2112: the first `dockhand outdated flyctl oh-my-posh`
after a rebuild printed nothing for 13 minutes and was killed; run again
with -vv it took seconds. A full PortIndex build and a wait on another
process's indexing were said already; the index cache's shared lock and
any network wait weren't. What it waited on is still unknown, so each wait
it could have been is said now.

- **`progress.After`** reports, once, at info level, what a step waits on
  where it runs longer than its delay; a step that ends sooner says
  nothing.
- **The PortIndex cache**: its shared lock, which only a cleanup of old
  indexes holds exclusively (retention's `Prune`), is said after three
  seconds: "Waiting for the PortIndex cache, which another dockhand is
  clearing of old indexes". flock names no holder, and writing a pid into
  the lock file would have a read-only cleanup's `TryExisting` write it,
  so the holder is said by what it does rather than by pid.
- **A port's lookup** that runs twenty seconds is said, once, on the
  command's own reporter rather than the lookup's quieted one: "Still
  looking up flyctl's newest release, waiting on its upstream".
- **outdated's count off a terminal**: a look piped or run by a script
  says "Looked up N of M ports' newest releases" once it has run ten
  seconds, and every thirty after, where only a terminal had the count.

Tests: TestAfterSaysASlowStepOnce, TestAWaitForTheCacheIsSaid,
TestALookOffATerminalSaysItsCountAsItGoes.
