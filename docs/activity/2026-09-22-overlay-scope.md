# 2026-09-22: an overlay holds the edited port, and every overlay is closed

## What was seen

The first live check of the main-port shared release, `bump atuin
--dry-run`, ran for seventeen minutes before it was stopped, at sixty
percent of a core, and left a thousand overlay directories under the
temporary directory, each a hardlinked copy of the whole ports tree.
Sampling it showed the time in `Overlay`, called from candidate
evaluation: discovery evaluates each of atuin's hundred-odd tags through
the Portfile, and each evaluation is an overlay of the workspace.

## Two defects

The overlay copied the base's whole scope. A base that generated the
index was widened to the whole tree, some hundred and twenty thousand
files, and each candidate then linked all of them. A Portfile's evaluation
reads its own directory and `_resources`, the same sparse projection the
base is opened with, so the overlay was linking a tree it would never
read. Before workspaces, the same evaluation wrote the candidate over the
Portfile in place; the overlay is what keeps the base immutable, and its
cost has to be the port's size, not the tree's.

Two overlays outlived the run. `dependencyBase` and
`checkSharedArchiveOwners` derive an input by copying the struct, and an
overlay made through the copy was appended to the copy's list, which
nobody closed. The dry run leaked two per invocation; a long-running
driver would have leaked them per preparation.

## The fixes

An overlay's scope is now the edits' port directories and `_resources`,
whatever the base holds, and `Ensure` widens an overlay as it widens a
base; `Scope` reports the overlay's own. A new test opens a whole-tree
base and checks that an overlay of one Portfile holds that port and
`_resources` and not its neighbour, then widens it by ensuring the
neighbour.

The input's overlays live in a holder the derived copies share by
pointer, so the original's `Close` removes every overlay its derivations
made.

## Measured

`bump atuin --dry-run` over the ports tree, same machine, after the fixes:
23 seconds, both members of the release listed, no overlay left behind.
The thousand directories the stopped run left are the user's to remove;
the sweep of a shared temporary directory is not one this session runs.
