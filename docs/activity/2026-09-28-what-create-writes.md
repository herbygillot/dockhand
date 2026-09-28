# 2026-09-28: what create writes

The second step of the private-helper review's item ([review](../reviews/2026-09-28-private-helper-ownership.md), findings 5 and 4; [reconciliation](2026-09-28-private-helper-review-reconciled.md)). Both were `newport` deciding a fact another package owns, and both wrote a wrong Portfile.

## A description, as a Tcl word (finding 5)

`newport.tclWord` wrote a description with a Tcl character in it by backslashing braces and backslashes, then bracing the whole. Inside braces Tcl keeps a backslash as it is, so `Tool {x}` reached MacPorts as `Tool \{x\}`, and `Tool C:\temp` gained a backslash.

Writing a Tcl word is now `tcl/syntax.Quote`, beside the reading of one, as Tcl's own list quoting writes an element:
- bare where nothing in it means anything to Tcl;
- braced where braces can hold it: they balance, a closing brace never comes first, and no backslash ends it or comes before a newline;
- otherwise with each character that means something backslashed.

An escaped brace doesn't count toward balancing, and a leading `#` is quoted, so the word can stand anywhere. `newport` keeps its own choice of plain words for a plain description, and quotes only the rest.

Tests:
- `TestAWordReadsBackAsItsValue` reads each of twenty values back through the package's own list reading;
- `TestTclReadsAWordAsItsValue` reads them back through MacPorts' Tcl;
- `TestADescriptionReachesTclAsWritten` runs `newport`'s descriptions through a `description` command in MacPorts' Tcl. It is the review's probe.

Mutations of each rule fail them. Two of them were caught only once the values `} {` and `\{}` were added.

## A Cargo.lock, read once (finding 4)

`create` and `update` each read a Cargo.lock their own way:
- update's generator checked each package's name, version, and checksum, knew crates.io in both its protocols, and refused a source it couldn't declare;
- create's took any `registry+` crate with a checksum as a crates.io crate, forgetting which registry. It skipped crates.io's sparse index, and dropped Git crates without a word.

The reading is now `dependency.ReadCargoLock`, taken out of the generator. It gives each package with where it comes from (local, crates.io, another registry, or Git) and its source as the lock writes it. It refuses what the generator refused: a lock format it doesn't know, a bad name or version, a crates.io crate without a valid checksum or pinned twice, and a source it can't place. Each command then decides what it can declare:
- **update's generator,** as before, refuses another registry, and handles Git crates by its policy for them;
- **create** writes the crates.io crates, the sparse index's included. A crate `cargo.crates` can't fetch is named and marked in the Portfile, and `cargo.crates` joins what the Portfile marks as unconfirmed:
  - "private-crate 1.0.0 comes from another registry, registry+https://example.org/index, which cargo.crates can't fetch";
  - "fork comes from Git, …; cargo2port writes its cargo.crates_github".

Tests:
- `TestACargoLockIsReadWithWhereEachPackageComesFrom` covers each source and each refusal;
- `TestACrateCargoCratesCantFetchIsMarked` is the review's probe, turned from an expected error into the mark;
- the existing generator tests pass unchanged.

The Git crate in `newport`'s fixture, which was dropped before, is now named.
