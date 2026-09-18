# One reader contract, and the lists that must agree

The second and third debts named on 2026-09-18.

## The editor requires an evaluator

The editor's reader was `macports.Reader`, whole-Portfile evaluation and target resolution, and eight places asked whether it could also observe, batch, or evaluate one port before choosing a path. The paths for a reader that could not were tested and never shipped: the product's one reader, `eval.Evaluator`, always could. `macports.Evaluator` is now the contract, whole and selected-only evaluation, modeled observation, and interpreter sessions together, with `NativeEvaluator` adding the native platform for assess and outdated. The editor, the preparation adapter, assess, and outdated require it; the upstream service's version selector requires native capture extraction as part of its contract rather than asking. The unobserved branches are gone: the plain archive plan that compared download sources by count, the plain checksum refresh, the assessment that skipped contexts, the revision reset that edited the literal without observing, and the shared-release refusals for a reader that could not observe. `portfile.ChecksumCount` and `portfile.ResetRevision` went with them; `make deadcode` named them. No test fake stood in for the editor's reader, so no test changed.

## The option lists

Three copies of one fact had drifted: the evaluator's Tcl field list, which options are read into a port's options; the fidelity allowlist, which of them may change with the version; and the livecheck and forge keys the source interpretation requires. Reading the evaluated options of a port showed the drift's shape: the allowlist named `distname`, `dist_subdir`, `github.master_sites`, and `gitlab.master_sites`, none of which the evaluator read, so those four entries had never done anything.

`macports/options.go` now holds them all: `ReadOptions`, given to the Tcl side at session start so the script has no list of its own; `InfoOptions` and `ComputedOptions`, the keys mportinfo and the evaluator itself supply; `VersionFollowers`, `LivecheckOptions`, `LivecheckListingOptions`, and `ForgeOptions`, which the fidelity check and the source interpretation read. A test holds every rule's list to the options that are actually read and refuses duplicates. `distname` and `dist_subdir` joined `ReadOptions`, since a bump names its source by them and fidelity now sees them move; the two forge master-site entries were dropped.

## The sqlite columns

Each column of the changes, pull requests, and jobs tables was spelled in a SELECT, an INSERT, and an UPDATE, with values in positional argument lists to match; adding the cleanup and observation columns this week meant editing three statements each and counting placeholders. A `table` names a record's columns once, builds the three statements from the list, and orders values and scan targets by column name, refusing a value with no column or a column with no value rather than shifting the rest into the wrong slot. The three tables that changed this week use it; the others can follow when they change.

## Tests

`macports`: the option lists. `sqlite`: the table helper's statements, ordering, and refusals, and every existing round trip. `portedit`, `preparation`, `assess`, `outdated`, `upstream`, `fidelity`, `source`, and `eval` pass with the branches removed and the options extended. The whole suite passes and `make deadcode` is clean.
