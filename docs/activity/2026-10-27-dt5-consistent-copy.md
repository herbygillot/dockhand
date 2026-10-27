# D-T5 copies the database consistently

2026-10-27, from Prime-time's rc6 quick stage, relayed by the coordinator
at 08:55Z. D-T5 failed "the truncated database was changed", a harness
fault: the stage's dockhand.db was 4096 bytes with no tables, everything
in its -wal, and the row's `cp` of the main file alone made empty valid
databases, which rc6 rightly initialised. On the M1, a consistent copy of
a real database truncated to 4096 bytes was refused, unchanged.

- The row copies with `sqlite3 <db> ".backup <copy>"`, which carries what
  the -wal holds.
- A copy no bigger than the truncation, 4096 bytes, is "not run": there is
  nothing for truncating to break.
