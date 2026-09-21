# The assess journal

The first half of queue item 1: a whole-tree `assess --all` that shows its progress and survives an interruption. The 2026-09-20 survey wrote one JSON document after four and a half hours, into a scratch directory that was later replaced, and its data had to be recomputed from the PortIndex.

`--journal <file>` appends one JSON line per port to the file as each port finishes. The first line names the assessed source, so a rerun against another commit is refused rather than mixing two trees' lines, and a rerun against the same commit reads the selectors the file holds and leaves them out, reporting how many. So an interrupted run continues where it stopped, and `wc -l` is the progress meter; the service also reports every thousandth port on stderr when a journal is set. Ports the index could not place, the "index-coverage" unknowns, are journaled like any other. The lines are the same `Port` objects the JSON envelope carries, flattened with their assessment, so a reader filters them with `select(.Selector)` and reads the source from the line that has none.

The journal lives in the `assess` package as a small type with a mutex, since ports finish concurrently, and the service asks it two things: whether it holds a port, and to record one. The envelope and the human summary still cover the run's own ports; the summary adds "N already in the journal" when a rerun skipped any. The test runs a port twice into one file and then rewrites the file's commit, checking the two lines, the skip, the summary sentence, and the refusal.

The second half, the run itself, is the baseline survey started the same night with this build, its journal kept under `~/.dockhand/surveys/`, outside the checkout.
