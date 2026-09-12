#!/usr/bin/env python3
"""Optional plots of the uninstrumented measurements; requires matplotlib."""
import csv
import json
import pathlib
import sys

import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt

root = pathlib.Path(sys.argv[1])
with (root / 'summary.csv').open() as f:
    rows = list(csv.DictReader(f))
plt.rcParams.update({'font.family':'DejaVu Sans','font.size':10,'axes.spines.top':False,'axes.spines.right':False})
fig, axes = plt.subplots(1, 2, figsize=(11, 4.6), layout='constrained')
for ax, operation, title in zip(axes, ['edit','read'], ['Change one existing job', 'Read the ledger']):
    for experiment, label, color in [('ledger-shared','One shared source','#147d92'),('ledger-distinct','Distinct sources','#b65321')]:
        selected = sorted([r for r in rows if r['experiment']==experiment and r['operation']==operation],key=lambda r:int(r['completed']))
        ok = [r for r in selected if int(r['successes'])]
        ax.plot([int(r['completed']) for r in ok],[float(r['median_ms'])/1000 for r in ok],marker='o',label=label,color=color)
        for r in selected:
            if not int(r['successes']):
                t = float(r['failure_ms'].split(';')[0])/1000
                ax.scatter([int(r['completed'])],[t],marker='x',s=65,color=color,zorder=4)
                ax.annotate('Timed out at 30 s\n(no completed update)',xy=(int(r['completed']),t),xytext=(-6,-27),textcoords='offset points',ha='right',fontsize=9,color=color)
    ax.set_xscale('log')
    ax.set_yscale('log')
    ax.set_xticks([10,100,1000,10000],['10','100','1,000','10,000'])
    ax.set_xlabel('Completed jobs retained (+ one active job)')
    ax.set_ylabel('Elapsed seconds, median')
    ax.set_title(title,loc='left',weight='bold')
    ax.grid(True,which='major',alpha=.18)
axes[0].legend(loc='upper left',frameon=False)
fig.suptitle('Dockhand v2 ledger baseline — Apple M5 Max / APFS',fontsize=14,weight='bold')
fig.savefig(root/'ledger-latency.png',dpi=180)
fig.savefig(root/'ledger-latency.svg')

fig, ax = plt.subplots(figsize=(7,4.4),layout='constrained')
for name,label,color in [('growth-10','10 completed jobs','#147d92'),('growth-1000','1,000 completed jobs','#b65321')]:
    data = [json.loads(line) for line in (root/(name+'.jsonl')).read_text().splitlines()]
    series = [r for r in data if r['kind'] in ('fixture','growth-storage')]
    ax.plot([r.get('sample',0) for r in series],[r['repository_bytes']/1024**2 for r in series],marker='o',color=color,label=label)
    compact = next(r for r in data if r['kind']=='growth-after-gc')
    value = compact['repository_bytes']/1024**2
    ax.scatter([100],[value],marker='D',color=color)
    ax.annotate(f'After Git GC: {value:.2f} MiB',xy=(100,value),xytext=(0.98,0.23 if name=='growth-1000' else 0.13),textcoords='axes fraction',ha='right',va='center',fontsize=9,color=color)
ax.set_xlabel('Actual ledger updates after the seeded snapshot')
ax.set_ylabel('Git directory regular-file bytes (MiB)')
ax.set_title('Storage growth from repeated small updates',loc='left',weight='bold')
ax.legend(frameon=False)
ax.grid(alpha=.18)
fig.savefig(root/'ledger-storage.png',dpi=180)
fig.savefig(root/'ledger-storage.svg')
