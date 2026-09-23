# 2026-09-23: the verification ledger

The third of the 2026-09-23 [review](../reviews/2026-09-23-architecture-review.md)'s
contracts, after [`state.Scoped`](2026-09-23-state-scoped.md) and the
[forge contracts](2026-09-23-forge-contracts.md).

## What changed

Each verification provider kept its own copy of the same bookkeeping
over the shared provider store: register a pool, take a per-request
file lock, read the request's row and refuse one another repository
wrote, write it back, and on reconciling a request the store had never
seen, write a closed row so no later submit could start it. Tart's copy
was an `operation` holding pool and lock with `read`, `put`, and
`close`; GitHub's was `locked(id, fn)` with `read` and `put` beside it.
Both read log bytes from an offset their own way, and their lock files
lived in different places.

`verify/ledger` is that bookkeeping once, a leaf whose import test
allows `record`, `state`, and `filelock` only:

```go
type Ledger struct {
	Store      state.ProviderStore
	Repository record.RepositoryID
}

func (l Ledger) Read(ctx, pool string, id record.RequestID) (record.ProviderExecution, error)
func (l Ledger) Open(ctx, pool record.ProviderPool, id record.RequestID) (*Entry, error)

type Entry struct {
	Pool record.ProviderPool
	Lock *os.File
}

func (e *Entry) Read(ctx) (record.ProviderExecution, error)
func (e *Entry) Put(ctx, record.ProviderExecution) error
func (e *Entry) View(ctx, func(ctx, state.ProviderReader) error) error
func (e *Entry) Update(ctx, func(ctx, state.ProviderTx) error) error
func (e *Entry) CloseUnknown(ctx, now time.Time) error
func (e *Entry) Close()

func LockPath(pool record.ProviderPool, id record.RequestID) string
func ReadChunk(path string, offset int64, limit int) (data []byte, next int64, eof bool, err error)
```

Tart's `operation` holds an entry in place of its pool and lock and
hands the entry's lock file to the guest machine as before; its
capacity check is the entry's `View`, and its capacity-checked reserve
is the entry's `Update`. GitHub's `locked` opens an entry and passes it
to the closure, so writes happen only under the lock by construction,
and its unlocked reads for observation and pruning go through the
ledger's `Read`. Both reconciliations call `CloseUnknown`; both log
readers call `ReadChunk` and keep their own meaning of complete, a
result present and a short read for Tart, the end of the file for
GitHub. Retention asks `LockPath` for the file it probes.

## The three choices, as made

- **What the shared rule is.** The ledger owns the row, the lock, the
  repository check, and the closed row for an unknown request. It does
  not own the answer to a closed or released row: Tart returns an error
  for a closed row and re-admits a released one that carries a result,
  GitHub refuses both with the stored rejection detail, and making
  those one would change a provider's behavior. That stays a decision
  of its own.
- **One lock path.** The ledger's convention is `locks` under the
  pool's directory, which Tart already used, so GitHub's request locks
  moved into a subdirectory of its coordination directory. Lock files
  are transient; the one cost is a window during a binary upgrade, when
  a resident driver of the old version and a command of the new one
  would not see each other's request locks until the driver restarts.
- **The rejection detail stays opaque.** GitHub stores a rejection
  reason in a closed row's result and Tart stores nothing there. The
  ledger carries the result as bytes, as the record does.

## Tests

The ledger has its own tests against a SQLite store: the lock is held
while an entry is open and free once it closes, a row another
repository wrote is a conflict, the closed row for an unknown request
is what it should be, and a chunk read reports the end of the file.
Both providers' suites pass unchanged in what they assert; two Tart
tests and two GitHub tests reach the ledger through the operation or
the entry where they reached the provider's own methods, and GitHub's
retention test takes the lock at its new path.
