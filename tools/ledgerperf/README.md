# Ledger performance experiments

This tool measures the existing implementation. It does not run builds, contact providers, modify a ports tree, or optimize ledger behavior. Each experiment creates and removes its own temporary Git repository. Ordinary Dockhand commands do not import the fixture package.

Build from the repository root:

```sh
go build -o /tmp/dockhand-ledgerperf ./tools/ledgerperf
python3 tools/ledgerperf/overlay.py "$PWD" /tmp/dockhand-perf-overlay
go build -overlay /tmp/dockhand-perf-overlay/overlay.json -o /tmp/dockhand-ledgerperf-diagnostic ./tools/ledgerperf
```

Run a small baseline:

```sh
/tmp/dockhand-ledgerperf -records 10,100 -sources shared,distinct -samples 5
```

Run the full recorded experiment sequence, then summarize it:

```sh
python3 tools/ledgerperf/run.py --binary /tmp/dockhand-ledgerperf --diagnostic /tmp/dockhand-ledgerperf-diagnostic --out /tmp/dockhand-performance
python3 tools/ledgerperf/summarize.py /tmp/dockhand-performance
```

Use a new output directory for each complete run. `--only name1,name2` runs selected cases and appends them to an existing manifest; existing case output is never overwritten. The complete sequence includes deliberate timeout cases and takes many minutes. JSONL output is flushed after each sample. A measured timeout is a result; malformed fixtures or failed setup stop the experiment. After an operation fails at a given size, its remaining repetitions are skipped. The run manifest preserves exact commands, durations, and exit codes.

## Workload and timing

Each completed job has a change, revision, request index, verification plan, attempt with build/test evidence and an external log reference, and released resource. Active jobs have running attempts and active resources. All jobs use one target and configuration. There are no real PRs, multiple-attempt retries, or large diagnostic payloads. Sources contain one small Portfile; this is not a copy of the MacPorts repository.

`-records` counts completed jobs; `-active` adds running builds. `-sources shared` reuses one source across all jobs, `distinct` uses one per completed job, and an integer selects an intermediate count. Active jobs reuse these sources. Each distinct source has a distinct commit and tree, with one common base commit. Source commits form a chain, as successive edits normally would. There are two ledger pins per distinct source in this fixture. A separate fixture-only ref retains one additional source for the new-revision measurement.

Fast-import generates source objects efficiently outside the timer. Setup writes a validated initial snapshot and pins directly rather than taking thousands of measured-path transactions to seed it. Fixed-size samples restore the same initial ledger ref, remove added pins, warm-read the snapshot, and run Go GC before timing. Reset does not prune loose objects left by preceding samples. Unpacked here means no post-seed maintenance: fast-import may already pack the source objects.

Normal operations use the real ledger, real Git executable, and the existing fsync settings. User Git configuration is isolated; automatic GC is disabled in fixture repositories so explicit maintenance comparisons are repeatable. `-pack` runs Git GC after setup and before sampling. Neither benchmark execution nor maintenance touches the user's source repositories.

`-history` adds synthetic commits sharing the exact same state tree to isolate ancestry depth from current state size. It is not a model of accumulated snapshots. `-growth` instead performs actual ledger updates without resetting, records storage at milestones, and runs Git GC afterward. Repository bytes are the sum of regular file lengths under the Git common directory, not allocated filesystem blocks or total disk writes.

The ordinary binary provides headline wall latency and parent-Go-process allocation counts. Those allocation counts are not peak memory and exclude Git subprocess memory. Fixture generation, snapshot reset, warm reads, GC, and memory-stat collection are outside the reported interval. All results are warm-cache measurements on the local machine; no cold-cache claim is made.

The optional `plot.py` script renders `summary.csv` and growth samples with matplotlib:

```sh
python3 tools/ledgerperf/plot.py /tmp/dockhand-performance
```

Matplotlib is only a plotting dependency; it is not needed to collect or summarize measurements.

## Operations

| Operation | Measured work |
| --- | --- |
| `read` | Decode and validate a ledger snapshot |
| `noop` | Update callback returns without changing records |
| `edit` | Change one existing job's detail |
| `new-revision` | Add a revision using a previously unpinned, already-local source |
| `status` | Project an all-jobs workflow status |
| `submit` | Accept a verification request selecting an existing revision |
| `observe` | Cycle scoped to active jobs, provider reports still running |
| `complete` | Cycle scoped to active jobs, provider reports passed; cleanup confirms release |
| `cycle-all` | Cycle across all completed and active jobs/resources |
| `codec-encode`, `codec-decode` | In-memory codec work, including its validation |

The provider performs no I/O or sleeps and does not probe the ledger. `-active 0 -ops cycle-all` measures a ledger with no actionable jobs or resources. Scoped operations exclude completed jobs from advancement, but the store still reads and writes the whole state.

`-writers N -readers M` starts separate OS processes against the same ledger and repository-scoped writer lock under the fixture’s `locks` directory. All workers open the store and wait at an input barrier before starting. Each issues `-samples` operations sequentially. This is a burst of closed-loop clients, not an open-loop arrival-rate capacity test. Readers generally finish before writers because reads are faster. Failed transactions remain in the output; they are not retried by the harness. All workers use the production five-second lock timeout and thirty-second operation timeout. `-budget` provides a separate outer deadline; the recorded ledger cases use 45 seconds to expose the internal thirty-second limit, while full-cycle probes use a ten-second experimental cutoff.

For concurrent drivers, add `-worker-op cycle-all` with `-writers`, `-active`, and the normal fixture options. Each writer runs real all-jobs workflow cycles through its own store and the instant observation provider; readers remain snapshot readers. Samples include an `advanced` count from `CycleResult.Advanced`. A successful cycle may advance nothing because another driver owns the claim or a retry is not due. Cycle completion rate is therefore not build throughput. These cases do not exercise VM submission or real provider capacity.

## Diagnostic build

`overlay.py` generates temporary replacements for two Go source files using Go's build overlay. It does not modify the checkout. The generated binary times Git calls and transaction phases: lock acquisition, lock ownership through release, read, encode-before, encode-after, and source-pin processing. It emits events only while a measured operation is active. Phase totals overlap: Git calls are contained in their enclosing ledger phases, and all transaction phases except lock acquisition are contained in lock ownership. Do not sum every phase into a total.

The diagnostic build adds clock reads, environment checks, and stderr output. Use it to attribute cost and count commands, not as the headline performance baseline. `summarize.py` associates events with samples by PID and timestamps and produces `phases.csv`. It also generates `summary.csv` with successes, failures, medians, minimums, maximums, and allocations. Three or five repetitions do not establish a reliable tail percentile, so the summary deliberately does not label their maximum as a production p95.

## Go benchmarks and checks

```sh
go test -run '^$' -bench BenchmarkLedger -benchmem -benchtime=3x ./internal/ledger
go test -run '^$' -bench BenchmarkWorkflow -benchmem -benchtime=1x ./internal/workflow
go test -race ./...
```

The standard benchmarks cover smaller reproducible cases next to their packages. The separate harness supplies larger cases, bounded failures, contention, history, and storage experiments. Run correctness tests separately from timing runs. The fixture tests verify a complete verification-and-cleanup cycle, exact snapshot reset, added-pin removal, and preservation of state and reserved source objects through history generation and packing.
