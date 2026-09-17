# Foreground and publication by default

Decided with the user on 2026-09-17: the change commands stay in the foreground and publish by default, because dockhand's purpose is to turn a bump into a PR and the durable job with Ctrl-C detachment already makes staying attached safe.

## The rule

Commands wait; `--detach` doesn't. `bump`, `bump-revision`, `refresh-checksums`, `amend`, and `rebase` prepare, verify, and publish, staying attached until the PR is confirmed. `verify` and `publish` stay through their completion. `--detach` returns once the work is accepted and admitted, leaving `wait` or `start` to finish it, which is what the old default did. `--trace` keeps implying completion and excludes `--detach`.

The destination is chosen by two stop-early flags: `--no-publish` stops after verification, and `--no-verify` stops at the prepared branch and therefore implies `--no-publish`, since publication requires passing verification. `--wait` and `--publish` are removed from these commands rather than kept as aliases; `cancel --wait` is unchanged, since cancel's default remains one cycle.

## Failing early

A default publication that cannot be bound is refused before any preparation: the workflow wraps the publisher's preflight and destination errors in `workflow.ErrPublicationIntake`, and the CLI adds "pass --no-publish to stop after verification" to that refusal. No GitHub login, no fork, and an ambiguous remote layout all fail this way, so a ten-minute verification never ends with "cannot publish".

When no verification provider is usable, the existing behavior stands: the job prepares the branch, records the setup requirement, and ends needing attention, so the default bump exits 3 rather than quietly succeeding without a PR. `--no-verify` is the deliberate way to stop at the branch.

## What changed

`internal/cli`: `Options` gained `Detach` and `NoPublish` in place of `Wait` and `Publish`; the change, correction, verify, and publish commands compute their milestone as completion unless detached and their destination as published unless told otherwise; the "destination flags require --publish" check became "need publication". Tests that relied on admission-only returns pass `--detach`; tests that wanted verification without a PR pass `--no-publish`. Every doc that showed `--wait` or `--publish` was rewritten: usage, CLI design, GitHub verification, human corrections, components, architecture, output, and the target workflow.
