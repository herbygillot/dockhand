# 2026-09-21: a contribution may change shared files beside its port

## Why

A port's update sometimes needs the PortGroup it loads to change with it,
and such branches are common in macports-ports. Dockhand refused them in
five places, each asserting "one port directory" in its own words: the
correction path's scope check before amend, rebase, and prepare-onto;
`adopt` for a branch and `adopt --pr`; publication's derivation of the
port from the changed paths and its content check; and the GitHub
verification provider's change filter. The workspace work had already
made the machinery ready: an overlay carries the edited group since
`_resources` is in every scope, the index takes a full pass for such a
tree, Tart staging ships the whole tree, and the working-tree capture
takes the edit once it is staged.

## The rule, written once

`changeset.ScopeOf(paths)` reads the one port directory a set of changed
paths belongs to, allows files under `_resources` beside it, and refuses a
second port directory, a path outside any port directory, and a change
under `_resources` alone, which names no port to prepare or verify. Its
errors read as predicates of the contribution, "changes devel/a and
devel/b; one port directory is supported", so each caller names the
subject: the pull request, the contribution. `Scope.Within` is the same
rule for a path against a known port, which publication's content check
and the GitHub provider use.

The five sites call it. The correction path additionally requires the
scope's directory to be the tracked port's. The GitHub provider's
guarantee is unchanged: nothing under `.github` can change, so the
workflow that runs is still upstream's own.

## What is not yet said

Verification builds the target only, and a PortGroup change affects every
port that loads the group. The next change lists those ports, from a
search of the candidate tree's Portfiles, in `status` and the pull request
body beside "Tested on", as a note rather than a refusal.

## Evidence

The scope rule's table test; publication tests for a group edit beside the
port and for a group-only commit; an adopt test with both; the inference
test's shared-resource scenario moved from the refusals to an acceptance
of its own.
