# SQLite state measurements — 2026-09-12

The final probe recorded 960 operation samples with zero errors. Updating one job and running idle cycles remained approximately flat across 100, 1,000, and 10,000 completed jobs in these fixtures. Four separate driver processes also operated on one active job without operation failures.

## Method

The [probe](../../tools/stateperf/README.md) seeds completed jobs, attempts, passing evidence, admitted submissions, and released resources. Shared-source fixtures reuse one revision; distinct-source fixtures give each historical job its own source/revision. IDs are synthetic: these measurements exercise SQLite and workflow progression, without Git, real providers, network traffic, or VM builds. Fixture creation and store opening are excluded from operation timings.

Each ordinary case has 20 samples. The concurrent case starts four child processes with 20 cycles each against the same database and active job, measured after opening. A cycle can lose eligibility to another driver; raw results distinguish advancement from successful no-op passes. The provider reports capacity waiting, preserving the same submission identity. WAL and full commit durability use normal backend defaults.

Platform: go version go1.27.1 darwin/arm64. The [manifest](2026-09-12-sqlite-state/manifest.json) records the command, source fingerprints, and environment.

## Final medians

Times are milliseconds per operation.

| Completed jobs | Sources | Job write | Idle cycle | Selected status | Active cycle | Four-process cycle |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 100 | Shared | 0.155 | 0.159 | 0.236 | 0.808 | 0.797 |
| 100 | Distinct | 0.132 | 0.119 | 0.222 | 0.727 | 0.746 |
| 1000 | Shared | 0.126 | 0.121 | 0.210 | 0.713 | 0.699 |
| 1000 | Distinct | 0.112 | 0.126 | 0.220 | 0.616 | 0.667 |
| 10000 | Shared | 0.104 | 0.124 | 0.208 | 0.659 | 0.692 |
| 10000 | Distinct | 0.117 | 0.124 | 0.242 | 0.645 | 0.685 |

At 10,000 distinct sources, the four-process cycle p95 was 2.485 ms. This measures contention and reconciliation overhead, not provider capacity or build throughput.

## Query correction

The [initial measurements](2026-09-12-sqlite-state/before-query-fix.jsonl) exposed remaining scans: at 10,000 distinct sources, median idle cycles were about 6.0 ms and selected status about 8.1 ms, while writes were already about 0.11 ms. SQLite query plans showed cancellation lookup traversing job requests and resource selection/cleanup traversing released history.

Added indexes for control requests and outstanding cleanup, and made selected-resource queries start from the selected jobs’ attempts. The final measurements above include those changes. No transaction or busy timeout was increased.

## Interpretation and limits

These fixtures establish that the new write path does not decode or serialize the entire stored history, and that settled historical work does not require a linear scan during idle cycles. They do not establish constant cost for every operation: repository-wide status enumerates its requested output, and reporting pending cleanup still enumerates outstanding retained/uncertain resources. More active jobs and larger evidence documents also increase the work requested.

The [prior Git-ledger report](2026-09-12-performance-pass.md) remains historical comparison evidence. The old and new probes use different storage representations and workloads, so their timings should not be treated as a controlled speedup ratio. Cold startup, backups, long-term maintenance, real provider execution, and large active dependent cohorts are not measured here.

Raw [final samples](2026-09-12-sqlite-state/final.jsonl) and [summary statistics](2026-09-12-sqlite-state/summary.json) accompany this report. Cross-process correctness is separately tested through concurrent updates, driver claims, and process termination during a write.
