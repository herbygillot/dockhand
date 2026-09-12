# SQLite state performance probe

Run `go run ./tools/stateperf -sizes 100,1000,10000 -iterations 20` from the repository root. It writes JSONL measurements to stdout and uses disposable databases. Build a binary first when measuring repeated runs.

Each fixture contains completed jobs, revisions, attempts, passing evidence, admitted submissions, and released resources. Shared-source fixtures reuse one revision; distinct-source fixtures give every historical job its own source and revision. Synthetic object IDs avoid Git work in a storage measurement. Seeding is excluded from timings.

The probe measures one-job writes, idle repository cycles, selected-job status, and capacity-waiting cycles. Four child processes then run cycles against the same active job and database. Each child measures after opening its store; startup is excluded. The JSON includes successful advancement separately from cycle latency, since a contending cycle may find no eligible work. The provider is scripted; no VM or network operations occur.

This replaces the executable Git-ledger harness. The former harness and implementation remain reproducible from commit `ec812d2`; historical measurements remain under `docs/performance`.
