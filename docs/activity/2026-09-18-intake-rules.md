# Intake: one rule per action, one prologue per bind

The first of three debts named on 2026-09-18: the engine's intake functions, each a long run of validation, resolution, and spec assembly with the same shape and no shared skeleton.

## normalizeSpec

The 203-line validator read "if the action is X" in eleven places, so what an action accepted was scattered through checks on other parts of the spec. It is now an action-rule table and twelve steps. `actionRules` has one row per action: whether it prepares a candidate tree, which destinations it may request, and which optional parts of a spec belong to it, such as the version only a bump selects, the shared release only a bump authorizes, the fresh verification and checkout provenance only a verify carries, the correction an amend or rebase requires, and the publication intent only a publish holds. Each step owns one part of the spec, from encodings and option flags through destination policy, publication, builds, requirements, version, preparation, checkout, source selection, and targets, and reads the rule for what the action allows. Every message is the one it was, and every check's condition is the same expression with the action literal replaced by the rule's column; the one visible change is that an unknown action is refused first rather than after the flag checks. Adding an action is a row.

## Bind prologues

Four bind functions opened by looking up the registered repository and comparing it to the engine's scope, and three of them resolved a publication destination through the publisher's preflight with their own timeout handling, one of them without a timeout at all. `requireRepository` and `publicationDestination` are the two helpers; each bind keeps its own wording of the refusal. The correction bind now resolves its destination within the publication timeout like the others.

## What is not done

The bind functions' bodies remain long: verification's is the resolution of a checkout or branch into a source and build, publication's the pairing of a change with its evidence, and each is one operation's logic rather than a shared skeleton. The review's intake/driver split, which would move them behind an intake type, stays deferred until a fourth bind shows what the skeleton is.

## Tests

`workflow`: the rule table covers every action, and one case per column exercises what a bump, revision bump, verify, publish, and amend accept or refuse, by message, without a submission per case; the submission, correction, verification, and publication tests pass unchanged. The whole suite passes and `make deadcode` is clean.
