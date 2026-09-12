# Ledger and driver-cycle performance pass — 2026-09-12

The performance pass removes per-source Git process spawning and write transactions for known ineligible cycle work. A concurrent-driver experiment also exposed writer starvation; an acquisition gate now lets a waiting process reserve the next resource handoff. The five-second lock timeout and thirty-second ledger operation timeout are unchanged.

## Before and after

Medians of successful warm-cache samples on the same Apple M5 Max host. Source counts refer to distinct snapshots retained in current records. Rows with one shared source say so explicitly. The [original baseline](2026-09-12-ledger-baseline.md) remains unchanged.

| Operation | Original baseline | Final implementation |
| --- | ---: | ---: |
| Small update, 100 sources | 849.3 ms | 230.7 ms |
| Small update, 1,000 sources | 8.58 s | 524.1 ms |
| Small update, 10,000 sources | Timed out at 30 s | 5.65 s |
| No-op update, 1,000 sources | 8.70 s | 135.7 ms |
| No-op update, 10,000 sources | Timed out at 30 s | 1.30 s |
| Idle cycle, 100 completed jobs / one source | 11.61 s | 28.2 ms |
| Observe one active job / 1,000 sources* | 30.92 s | 1.11 s |
| Complete and release one active job / 1,000 sources* | 38.73 s | 2.18 s |
| Observe ten active jobs / 100 sources | 27.57 s | 2.69 s |

*The 1,000-source workflow cases have one sample each in both runs; they are individual measurements, not reliable estimates of a median distribution. Most other operations use three or five repetitions. Timed-out baseline cases are lower bounds, not completed latencies.*

An idle cycle with 10,000 completed jobs sharing one source now takes 214.8 ms. A small update at 10,000 distinct sources still takes seconds: this pass improves scaling without making writes independent of retained state size.

## Mechanisms and concurrency guarantees

`git.Repository.CommitTrees` checks original object types and resolves root trees through one batch process. It accepts immutable IDs and rejects tags, missing objects, and malformed responses. The ledger compares resolved trees with recorded source trees, checks all referenced source objects, repairs missing pins on otherwise unchanged updates, and guards the state ref and required pin refs in the same publication transaction. There is no validation cache or relaxed retention rule.

Workflow cycles share snapshot capture and scope validation with status, while avoiding construction of the public status projection. They preselect pending controls, unfinished jobs, and eligible cleanup resources. Live claims, future retries, settled work, and active or unexpired retained resources are skipped. Queued cancellation can bypass its retry delay. Missing cleanup ownership remains diagnosable. Each handler rereads current records and acquires its claim transactionally; stale candidate selection never grants authority to call a provider. Cleanup remains independent of job completion.

Provider capabilities are requested only when a candidate exists. Snapshots refresh after handlers to incorporate newly admitted resources and same-pass completion/cleanup. Work becoming eligible after selection can wait for the next cycle.

The diagnostic 1,000-source update launched **11 Git commands**, versus 1,010 before. It held the writer lock for **509.7 ms**, versus 8,291.5 ms. Pin processing fell from 7,782.9 ms to **65.5 ms**. A scoped running observation uses **2 transactions** and **30 Git commands**, versus four transactions and 4,044 commands before. The idle ten-job/distinct-source case uses **4 Git commands and no write transaction**, versus 348 commands and 21 no-op transactions.

## Multiple processes

The original writer/read burst cases use the same fixtures, operation counts, and deadlines as before:

| Retained sources | Writer processes | Before: successful writes | After: successful writes | After: median successful write |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 1 | 10/10 | 10/10 | 127.0 ms |
| 100 | 2 | 19/20 | 20/20 | 283.0 ms |
| 100 | 4 | 31/40 | 40/40 | 429.7 ms |
| 1,000 | 2 | 4/8 | 8/8 | 1.12 s |

New experiments run real all-jobs workflow cycles in separate driver processes against the same ledger, using an instant observation provider. In the first implementation round, four drivers with 100 sources and ten active jobs suffered **7/20 cycle failures** from writer-lock acquisition timeouts. Short transactions alone did not prevent a rapid writer from repeatedly reacquiring before polling waiters.

Every local resource lock now has a companion `.gate` file. A waiter holds this gate until it acquires the resource lock, then releases the gate while retaining the resource descriptor. A holder finishing work cannot reacquire ahead of a waiter already holding the gate. Both acquisitions share the existing deadline and cancellation handling; files remain in place after release. Gate contenders do not receive a strict FIFO guarantee.

With the gate, the four-driver case completed **60/60 cycles** across three independent runs without a timeout: trial 1: 20/20, median 5.75 s; trial 2: 20/20, median 5.24 s; trial 3: 20/20, median 5.76 s. Two drivers with 1,000 sources and four active jobs completed **6/6 cycles** without errors; their median cycle took 4.61 s.

A successful cycle can advance no work because another driver owns the claim or a retry is not due. Raw `advanced` counts are handler reports and can include partial progress in a failed cycle. These experiments establish neither build throughput nor a production tail-latency guarantee. Fairer handoff also distributes waiting among writers: a higher median for an individual writer is not, by itself, evidence of reduced aggregate throughput. The failed pre-gate cycles must not be excluded when comparing reliability.

## Remaining costs

- A write still reads, validates, and serializes the complete state. At 10,000 jobs sharing a source, one edit allocated approximately **481 MB** in Go. This is cumulative allocation, not peak memory, and excludes child Git processes.
- Every required pin still participates in the final ref transaction. In the 1,000-source diagnostic update, `git update-ref` took **321.4 ms**, about **63%** of lock ownership. Incremental pin management needs an explicit retention/concurrency design before dropping those assertions.
- Candidate selection scans the current snapshot. Actual advancement still incurs per-action transactions and whole-state work; source batching is not constant-time validation.
- The gate limits the observed reacquisition problem, not the number of processes or amount of work that one serialized ledger can sustain. Larger workloads can still exhaust configured deadlines.

## Validation and reproduction

`go test -race ./...` and `go vet ./...` passed. New tests cover batch lookup in both Git object formats, invalid/malformed responses, idle cycles under a held writer lock, cancellation timing, partial controls, stale candidates, three writer subprocesses preserving accepted updates, and a queued waiter receiving the next lock handoff. Existing pin repair, garbage collection, stale-claim recovery, uncertain submission, cancellation, and cleanup tests remain passing.

The final measurements comprise **22 experiments, 486 ordinary samples, and 35 diagnostic samples**, with **zero measured operation failures**, taking 7.4 minutes in aggregate. An earlier 20-experiment round with seven failures is retained for diagnosis. Tests and builds finished before timed runs; experiments ran sequentially. The machine was not reserved exclusively for benchmarking. Fixtures, warm reads, and GC follow the original harness; there were no real MacPorts builds, VM operations, forge calls, or edits to user ports trees. History/packing/growth experiments were not repeated because this pass does not alter that storage model.

Use the [reproduction guide](../../tools/ledgerperf/README.md). The [final manifest](2026-09-12-performance-pass/final/manifest.json) records the 20 selected case commands; repeat `driver-contention-100` into fresh directories for the two additional runs. [Final summary](2026-09-12-performance-pass/final/summary.csv), [diagnostic phases](2026-09-12-performance-pass/final/phases.csv), [machine/build metadata](2026-09-12-performance-pass/machine.json), and [source hashes](2026-09-12-performance-pass/final/source-sha256.json) accompany raw outputs. `before-gate/`, `repeat-2/`, and `repeat-3/` preserve the other rounds and their manifests. The implementation is the recorded working tree on top of `00a6b6b`, including the preceding lock-directory change.

Human-edit adoption and user-facing commands were not changed; that workflow remains a separate design discussion.
