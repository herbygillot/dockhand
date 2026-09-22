# 2026-09-22: the survey rerun with symlinked resources

The whole-tree survey ran a third time on commit `842164fd61d`, with the
overlay sharing `_resources` by symlink, overlays reused for identical
contents, and the observer knowing the base root, the fixes the
[survey note](2026-09-22-survey-after-workspaces.md) describes. The
index was already cached, so nothing was built first.

| | 2026-09-21 baseline | 2026-09-22 first rerun | 2026-09-22 this rerun |
|---|---|---|---|
| wall time | 34 min 27 s | 80 min 19 s | 48 min 49 s |
| CPU user | 12,517 s | 14,043 s | 12,566 s |
| CPU sys | 9,686 s | 21,105 s | 12,121 s |
| CPU total | 22,203 s | 35,148 s | 24,687 s |
| cores busy | 10.7 | 7.3 | 8.4 |

Every one of the 41,733 ports has the outcome and the findings the first
rerun gave it: 39,283 input-found, 1,481 unsupported, 969 unknown, and
the 195 discovery gains over the baseline.

CPU is back within 11 percent of the baseline; the 2,400 seconds of
kernel time still above it are the overlays that remain, a few small
files each, and the run root's directories. Wall time is 42 percent
above the baseline, and that is a loss of parallelism, 8.4 cores busy
against 10.7, not of work. Where it went is not measured: candidates are
the workspace's mutex and the registry of open roots, taken on every
overlay and every ensure from eight workers, and the shared session's
serialization of evaluations against one base. It is on the roadmap as a
measurement to make before anything is changed for it.
