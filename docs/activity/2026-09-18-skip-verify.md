# Publishing without a build

`--no-verify` is now `--skip-verify` (`-V`), and it publishes. The old flag stopped at the prepared branch because the engine's rule was that publication requires passing verification. The rule is now that publication cites passing verification unless the author explicitly skipped it, and the flag is how the author says so.

## The four combinations

| Flags | What happens |
|---|---|
| none | prepare, verify, publish |
| `--no-publish` (`-P`) | prepare, verify, stop |
| `--skip-verify` (`-V`) | prepare, publish, with the PR saying no build ran |
| both | prepare, stop |

The same pair is on `amend` and `rebase`, and `publish` takes `--skip-verify` for a tracked contribution. `-N` is gone; the old name is not aliased.

## The job shape

A skip-verify job carries `Verification: skipped`, no build configuration, no build requirements, and the published destination. The validator's publication rules take that shape or the verified one, never a mix: a build with skipped verification, or skipped verification with dependents or a kept failed environment, is refused. The store's phase rule, which allowed a job to advance one phase at a time, has one exception: a job that skipped verification and publishes steps from preparation straight to publication, since it has no verification phase. Integration sends it there once the branch is recorded, unless a patch stopped applying, which still ends the job needing attention, now worded for publication rather than verification.

The publication record has a new `unverified` mark and no evidence attempt; the two are tied together in the validator, in the store's insert, and in a check constraint on the rebuilt `publications` table (schema 22), whose evidence column is nullable exactly for records whose spec declares itself unverified. The evidence policy short-circuits for such a record after confirming the job really skipped verification and cites nothing; coverage description skips it too. Standalone `publish --skip-verify` refuses an untracked branch, since without evidence there is nothing that names the port.

## What the PR says

The "Tested on" section reads: not built locally, the author asked dockhand to publish without verification, no lint, test, or install verdict exists, and the MacPorts pull request workflow is the only check it has had. The lint, test, and install checklist items are unchecked and say "skipped at the author's request". `status` shows the contribution as published unverified, and the job's completion says the publication was confirmed with verification skipped at the author's request.

## Tests

Workflow: a combined bump and bump-revision that skips verification runs preparation, publication, and confirmation with no attempts and a disclosing body; the refusals for builds, requirements, dependents, kept failures, and the wrong destination; standalone publish with the flag for a tracked branch and its refusal for an untracked one; amend with the flag both stopping at the branch and updating the PR. CLI: `bump-revision -V` through a fake GitHub, then `publish --dry-run` on the same branch needing evidence without the flag and disclosing with it; `-P -V` stopping at the branch; `--dependents` and `--trace` refused with it. Store: the phase skip is allowed only for a job accepted with skipped verification. Migration: a cited publication survives the rebuild, and the check constraint admits a null evidence column only for a declared unverified spec.
