# A revision's change records and assessments: one view, one transaction

2026-10-27, Then's item 15, its first part (SQL-O7's remainder, the docs
dig's I44), ahead of the comparison's move, at the architecture thread's
word on the item's shape (the coordinator's relay, 14:42Z).

- `revisionChanges` splits into three parts:
  - a read, `recordedChanges`, over a store reader;
  - a make, `makeChanges`, which evaluates outside any transaction and
    returns what it made;
  - a write, `recordChanges`, in the caller's transaction.
- `revisionAssessments` reads the branch, its change records, and its
  assessments in one `Store.View`. It makes change records and
  assessments outside any transaction, where the network and the
  evaluator are read. It writes both in one `Store.Update`. The change
  records it made are kept where no planner could make assessments, as
  before.
- **The write keeps another's**: each row is written only where the
  revision's row for its directory or port is still the one read before
  it was made, absent or the one being made again. Another process on the
  shared database may have recorded one meanwhile, and that one stands
  (the architecture thread's rule: "insert-where-absent").
- Test: TestARecordMadeMeanwhileStands.
