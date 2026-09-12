#!/usr/bin/env python3
"""Summarize raw samples and correlate optional overlay events by PID and time."""
import collections
import csv
import json
import pathlib
import statistics
import sys

root = pathlib.Path(sys.argv[1])
rows = []
phases = []
for path in sorted(root.glob('*.jsonl')):
    samples = [json.loads(line) for line in path.read_text().splitlines() if line]
    samples = [s for s in samples if s['kind']=='sample']
    groups = collections.defaultdict(list)
    for s in samples:
        key = (s['operation'],s['completed'],s['sources'],s['active'],s['history'],s['packed'],s.get('writers',0),s.get('readers',0))
        groups[key].append(s)
    for key, ss in groups.items():
        ok = [s for s in ss if not s.get('error')]
        row = dict(zip(('operation','completed','sources','active','history','packed','writers','readers'),key))
        row.update(experiment=path.stem,samples=len(ss),successes=len(ok),errors=len(ss)-len(ok))
        if ok:
            times = [s['ms'] for s in ok]
            row.update(median_ms=statistics.median(times),min_ms=min(times),max_ms=max(times),median_allocated_bytes=statistics.median(s.get('allocated_bytes',0) for s in ok))
        else:
            row.update(median_ms='',min_ms='',max_ms='',median_allocated_bytes='')
        row['failure_ms'] = ';'.join(f"{s['ms']:.3f}" for s in ss if s.get('error'))
        row['error_messages'] = ' | '.join(sorted({s['error'] for s in ss if s.get('error')}))
        rows.append(row)
    stderr = path.with_suffix('.stderr')
    if not stderr.exists():
        continue
    events = [json.loads(line[5:]) for line in stderr.read_text().splitlines() if line.startswith('PERF ')]
    for s in samples:
        grouped = collections.defaultdict(list)
        for event in events:
            if event['pid']==s.get('pid') and s['started_ns']<=event['start_ns']<=s['started_ns']+s['ms']*1e6:
                grouped[event['phase']].append(event['ns']/1e6)
        for phase, values in grouped.items():
            phases.append(dict(experiment=path.stem,operation=s['operation'],completed=s['completed'],sources=s['sources'],worker=s.get('worker',0),sample=s['sample'],error=s.get('error',''),phase=phase,count=len(values),total_ms=sum(values),min_ms=min(values),max_ms=max(values)))
for name, data in [('summary.csv',rows),('phases.csv',phases)]:
    if data:
        with (root/name).open('w',newline='') as f:
            writer = csv.DictWriter(f,fieldnames=list(data[0]))
            writer.writeheader()
            writer.writerows(data)
print(f'{len(rows)} result groups; {len(phases)} phase groups')
