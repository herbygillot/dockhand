# 2026-09-28: a stealth update's edits inside the editor

The third step of the private-helper review's item ([review](../reviews/2026-09-28-private-helper-ownership.md), finding 9; [reconciliation](2026-09-28-private-helper-review-reconciled.md)). It is the open half of the earlier code-organization review's finding 19.

## What was wrong

A stealth update is an archive whose contents changed upstream under the same name. For one, dockhand bumps the revision, since the source changed, and sets `dist_subdir` so mirrors keep both archives. A later version update removes the `dist_subdir`.

Those edits were the engine's (`engine/stealth.go`). They changed the Portfile after the editor had evaluated it and checked its fidelity, and rebuilt only the Git tree. The revision and directory they reported were computed from the earlier evaluation, not read from one of the final text. The revision bump always went to the Portfile's top level, whichever port the refresh selected. The earlier review had noted that, for subports, the bump was never proven.

## What changed

The editor makes these edits, as it raises a Go toolchain minimum, and checks them the same way:
- **A checksum refresh with `Request.Stealth`** finds the archives whose checksums changed under the same name. It bumps the selected port's revision, a subport's in its own block, unless asked to keep it, and sets `dist_subdir`. Then it evaluates the result, and holds it to changing that port's revision and `dist_subdir` alone. The directory it reports is read from that evaluation.
- **A version update** removes a stealth `dist_subdir` the same way: evaluated, and held to changing the selected port's `dist_subdir` alone. The removal is said in `Result.DistSubdirRemoved`.
- **What would change more** is left. For instance, a subport that inherits the revision would be bumped with the port. The checksums are still refreshed, and `Stealth.Problem` says what the edits would also have changed: "the revision and dist_subdir it would set also change fixture-extra.revision: expected 2, got 3; …, so they are left for you". Before, both were silently changed.

**What stays with the engine** is what the branch knows: which files it has changed since its base. It gives them to the editor (`StealthRequest.Changed`), since a Portfile among them was edited by hand first, as for a new version, and its refresh is no stealth update. `engine.Stealth` is now the editor's type, through `preparation`, and `engine/stealth.go` holds only that.

Preparation's final evaluation of the committed tree now covers these edits too, as it covers every edit the editor makes.

## Tests

The editor's, with MacPorts' own evaluator and a served archive, each failing with its part undone:
- `TestAStealthUpdateIsEvaluatedAsTheEditorMakesIt`;
- `TestAStealthUpdateKeepingTheRevisionNumbersTheDirectory`;
- `TestAStealthUpdateNeedsAskingAndAnUnchangedPortfile`;
- `TestAStealthUpdateThatWouldChangeASubportIsLeftForYou`;
- `TestANewVersionDropsTheStealthDistSubdirAsEvaluated`.

The command's stealth tests keep testing the engine's side and the words. Their stand-in preparers now do what the editor does, so "a version edited by hand first is not a stealth update" tests the files the engine gives. Nine mutations each fail a test.
