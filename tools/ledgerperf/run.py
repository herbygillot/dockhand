#!/usr/bin/env python3
"""Run the baseline sequentially so experiments do not compete with each other."""
import argparse
import datetime
import json
import pathlib
import subprocess

p = argparse.ArgumentParser()
p.add_argument('--binary', required=True)
p.add_argument('--diagnostic', required=True)
p.add_argument('--out', required=True)
p.add_argument('--only', help='comma-separated case names to run or resume')
a = p.parse_args()
out = pathlib.Path(a.out)
out.mkdir(parents=True, exist_ok=True)

cases = [
    ('ledger-shared', ['-records','10,100,1000,10000','-sources','shared','-samples','5','-ops','read,noop,edit,new-revision,status,codec-encode,codec-decode']),
    ('ledger-distinct', ['-records','10,100,1000,10000','-sources','distinct','-samples','3','-ops','read,noop,edit,new-revision']),
    ('workflow-shared', ['-records','10,100,1000,10000','-sources','shared','-samples','3','-ops','submit,observe,complete']),
    ('workflow-distinct', ['-records','10,100','-sources','distinct','-samples','5','-ops','submit,observe,complete']),
    ('workflow-distinct-1000', ['-records','1000','-sources','distinct','-samples','1','-ops','submit,observe,complete']),
    ('idle-shared', ['-records','10,100,1000,10000','-sources','shared','-active','0','-samples','3','-ops','cycle-all','-budget','10s']),
    ('idle-distinct', ['-records','10,100','-sources','distinct','-active','0','-samples','3','-ops','cycle-all','-budget','10s']),
    ('active-all', ['-records','10,100','-sources','shared','-samples','3','-ops','cycle-all','-budget','10s']),
    ('history-1000', ['-records','100','-sources','shared','-history','1000','-samples','5','-ops','read,noop,edit']),
    ('history-10000', ['-records','100','-sources','shared','-history','10000','-samples','5','-ops','read,noop,edit']),
    ('history-10000-packed', ['-records','100','-sources','shared','-history','10000','-pack','-samples','5','-ops','read,noop,edit']),
    ('sources-1000-packed', ['-records','1000','-sources','distinct','-pack','-samples','3','-ops','read,noop,edit']),
    ('contention-1', ['-records','100','-sources','distinct','-writers','1','-readers','1','-samples','10']),
    ('contention-2', ['-records','100','-sources','distinct','-writers','2','-readers','1','-samples','10']),
    ('contention-4', ['-records','100','-sources','distinct','-writers','4','-readers','1','-samples','10']),
    ('contention-1000', ['-records','1000','-sources','distinct','-writers','2','-readers','1','-samples','4']),
    ('growth-10', ['-records','10','-sources','shared','-growth','100']),
    ('growth-1000', ['-records','1000','-sources','shared','-growth','100']),
    ('diagnostic-shared', ['-records','1000','-sources','shared','-samples','1','-ops','read,noop,edit,observe,complete']),
    ('diagnostic-distinct', ['-records','1000','-sources','distinct','-samples','1','-ops','noop,edit,observe']),
    ('diagnostic-contention', ['-records','100','-sources','distinct','-writers','4','-readers','1','-samples','5']),
]
cases.extend([
    ('idle-shared-100-complete', ['-records','100','-sources','shared','-active','0','-samples','3','-ops','cycle-all','-budget','45s']),
    ('active-ten', ['-records','100','-sources','shared,distinct','-active','10','-samples','3','-ops','observe']),
    ('diagnostic-idle', ['-records','10','-sources','shared,distinct','-active','0','-samples','1','-ops','cycle-all']),
])
cases.extend([
    ('driver-contention-100', ['-records','100','-sources','distinct','-active','10','-worker-op','cycle-all','-writers','4','-readers','1','-samples','5']),
    ('driver-contention-1000', ['-records','1000','-sources','distinct','-active','4','-worker-op','cycle-all','-writers','2','-readers','1','-samples','3']),
])
if a.only:
    wanted = set(a.only.split(','))
    unknown = wanted - {name for name, _ in cases}
    if unknown:
        raise SystemExit(f'Unknown cases: {unknown}')
    cases = [(name, args) for name, args in cases if name in wanted]
manifest_path = out / 'manifest.json'
manifest = json.loads(manifest_path.read_text()) if manifest_path.exists() else []
for name, args in cases:
    path = out / (name + '.jsonl')
    if path.exists():
        raise SystemExit(f'Refusing to overwrite {path}; use another output directory')
    binary = a.diagnostic if name.startswith('diagnostic-') else a.binary
    command = [binary, '-budget', '45s', *args]
    print(f'{datetime.datetime.now().isoformat(timespec="seconds")} running {name}', flush=True)
    start = datetime.datetime.now(datetime.timezone.utc)
    with path.open('w') as stdout, (out / (name + '.stderr')).open('w') as stderr:
        completed = subprocess.run(command, stdout=stdout, stderr=stderr, timeout=1800)
    manifest.append({'name':name,'command':command,'start':start.isoformat(),'elapsed_seconds':(datetime.datetime.now(datetime.timezone.utc)-start).total_seconds(),'exit_code':completed.returncode})
    (out / 'manifest.json').write_text(json.dumps(manifest, indent=2)+'\n')
    if completed.returncode:
        raise SystemExit(f'{name} failed; inspect its stderr and partial results')
print('Measurements complete.', flush=True)
