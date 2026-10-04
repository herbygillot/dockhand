# 2026-10-04: a canceled BEGIN no longer leaves a transaction open

CI's Intel job at 90de4fc2 failed `TestAnEarlierResultFillsInOnlyForTheCommitExpected` with "storage unavailable: SQL logic error: cannot start a transaction within a transaction (1)". 2f0dce70, the same code, had passed, so it came and went. The Prime-time thread asked for the cause, since a store that can't begin a transaction is the "database stuck" harm.

## The cause

The store runs each transaction on a connection from the pool: `BEGIN` (or `BEGIN IMMEDIATE`), the work, `COMMIT`, and a `ROLLBACK` if it doesn't commit. modernc.org/sqlite, the driver, interrupts a statement whose context ends while it runs. Once that interrupt has fired, it reports the context's error, even where the statement had finished first (`stmt.exec`'s deferred check of its done flag). A `BEGIN` that took effect then read as failed. The store returned at once, before its deferred `ROLLBACK` was set, and the connection went back to the pool inside the transaction. The next transaction to draw that connection failed at its own `BEGIN`, and so would every one after it on that connection, until the process ended. A check's driver cancels contexts as its attempts end, which is how the engine's test met it on a slow runner. serve, which runs for days, could have met it too.

## What changed

A connection whose `BEGIN` reports an error is discarded rather than returned to the pool, as one whose `ROLLBACK` fails already was. A `COMMIT` reported failed after taking effect was already safe: the store reports the outcome as uncertain, its `ROLLBACK` finds no transaction, and the connection is discarded.

## Verification

- A new store test runs 3,000 transactions whose contexts are canceled at moments spread around their `BEGIN`, then a transaction on every connection the pool holds. Without the fix it fails within a few runs with the same error; with it, it passed 13 runs.
- The full checks: fmt-check, vendor-check, vet, lint, deadcode, and the suite under MacPorts.
