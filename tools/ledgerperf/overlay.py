#!/usr/bin/env python3
"""Generate a diagnostic Go overlay; never edit production source files.

Usage: python3 tools/ledgerperf/overlay.py /absolute/repo /absolute/output-dir
Build with: go build -overlay /absolute/output-dir/overlay.json ...
The instrumented binary emits phase timings to stderr only inside measured calls.
Use the ordinary binary for headline latency and the overlay only for attribution.
"""
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1]).resolve()
out = pathlib.Path(sys.argv[2]).resolve()
out.mkdir(parents=True, exist_ok=True)
replacements = {}


def replace(text, old, new):
    if text.count(old) != 1:
        raise SystemExit(f"instrumentation anchor must occur exactly once: {old!r}")
    return text.replace(old, new)


path = root / "internal/ledger/transaction.go"
text = path.read_text()
text = replace(text, '"fmt"', '"fmt"\n"os"')
text = replace(text, 'lock, err := s.lock(ctx)', 'lockStart := time.Now()\nlock, err := s.lock(ctx)\nperfPhase("lock-wait", lockStart)')
text = replace(text, 'defer lock.Close()', 'heldStart := time.Now()\ndefer func() { lock.Close(); perfPhase("lock-held", heldStart) }()')
for name, statement in [
    ('read', 'before, err := s.read(ctx)'),
    ('encode-before', 'original, err := Encode(before.State)'),
    ('encode-after', 'data, err := Encode(tx.State)'),
    ('pins', 'pins, pinsAdded, err := s.sourcePins(ctx, tx.State)'),
]:
    var = name.replace('-', '') + 'Start'
    text = replace(text, statement, f'{var} := time.Now()\n{statement}\nperfPhase("{name}", {var})')
text += '''
func perfPhase(name string, started time.Time) {
    elapsed := time.Since(started)
    if os.Getenv("DOCKHAND_PERF_ACTIVE") == "1" {
        fmt.Fprintf(os.Stderr, "PERF {\\"pid\\":%d,\\"phase\\":%q,\\"start_ns\\":%d,\\"ns\\":%d}\\n", os.Getpid(), name, started.UnixNano(), elapsed.Nanoseconds())
    }
}
'''
target = out / 'transaction.go'
target.write_text(text)
replacements[str(path)] = str(target)

path = root / 'internal/git/repository.go'
text = path.read_text()
anchor = 'func (r *Repository) run(ctx context.Context, input []byte, env []string, args ...string) ([]byte, error) {'
text = replace(text, anchor, anchor + '''
    perfStarted := time.Now()
    defer func() {
        elapsed := time.Since(perfStarted)
        if os.Getenv("DOCKHAND_PERF_ACTIVE") == "1" {
            fmt.Fprintf(os.Stderr, "PERF {\\"pid\\":%d,\\"phase\\":%q,\\"start_ns\\":%d,\\"ns\\":%d}\\n", os.Getpid(), "git:"+args[0], perfStarted.UnixNano(), elapsed.Nanoseconds())
        }
    }()
''')
target = out / 'repository.go'
target.write_text(text)
replacements[str(path)] = str(target)
(out / 'overlay.json').write_text(json.dumps({'Replace': replacements}, indent=2) + '\n')
print(out / 'overlay.json')
