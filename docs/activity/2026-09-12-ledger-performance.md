# Ledger performance baseline — 2026-09-12

Implemented measurement infrastructure and collected a baseline against production commit `00a6b6b35dcb`. No production ledger, workflow, Git, CLI, or module dependency changes were made.

## Added

- `internal/perftest`: deterministic source and record fixtures in disposable Git repositories, snapshot reset and synthetic-history helpers, and an instant verification provider. Fixture tests verify completion/cleanup, exact reset, pin removal, and preservation through history generation and packing.
- `internal/ledger/benchmark_test.go` and `internal/workflow/benchmark_test.go`: opt-in Go benchmarks covering ordinary operations and complete cycles.
- `tools/ledgerperf`: a standalone measurement harness for independent record/source sizes, actual history growth, Git packing, and separate-process writer/reader contention. It retains errors and bounds each operation.
- A diagnostic Go build overlay that measures lock wait/ownership, ledger phases, and Git command count/time without editing production code. The uninstrumented binary supplies the headline timings.
- Reproduction, summarization, and optional plotting scripts; a methodology document; raw measurements and a report under `docs/performance`.

All fixture, provider, benchmark, harness, overlay, analysis, plotting, and report code/text was authored for this work. Nothing was copied from v1, including tests or comments. Matplotlib was installed only into a temporary environment under `/private/tmp` to render the charts; no Go dependencies were added.

## Findings

The 24 experiments produced 640 uninstrumented samples and 35 diagnostic samples. A small update at 1,000 distinct retained sources took 8.58 seconds, compared with 168 ms for a shared source. At 10,000 distinct sources, no-op and changing updates exhausted the default thirty-second operation limit. A diagnostic update at 1,000 sources launched 1,010 Git commands and spent about 94% of writer-lock ownership checking source pins.

Workflow cycles amplify that cost: one running observation uses four transactions, and an idle cycle over ten completed jobs and released resources uses 21 no-op transactions. Concurrent writers hit the five-second lock timeout even at 100 distinct sources. Reads remained responsive. Holding the current snapshot fixed while extending Git ancestry by 10,000 commits did not materially affect latency; Git GC substantially reduced storage but did not remove source-validation latency.

See `docs/performance/2026-09-12-ledger-baseline.md` for measurements, scope, limits, and proposed next steps. No proposed optimization was implemented.

## Validation

- `go test -race ./...` passed, including the new fixture tests.
- `go vet ./...` passed.
- Standard ledger and workflow benchmarks were smoke-tested with one iteration at ten completed jobs.
- All 24 experiment invocations exited successfully; measured operation failures remain in their JSONL results.
- Python script syntax and the generated charts were checked.

Performance experiments ran sequentially. Correctness checks and benchmark smoke tests were kept outside the reported measurement sequence.
