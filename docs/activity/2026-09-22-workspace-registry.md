# 2026-09-22: one workspace per source, shared through a registry

## The step

Step 5 of the [workspace design](../workspace-design.md): a registry that
hands out one workspace per source within a process and closes it when
the last holder releases it, adopted where `git.Materialize` was still
called directly. Five sites did: Tart staging, dependent discovery, the
survey behind assess and outdated, the engine's snapshot binding, and
index generation. Four now acquire from the registry; index generation
keeps its private full materialization, because it sets every Portfile's
mtime for the incremental protocol and that metadata is shared through
hardlinks, as the design's "what writes into a root" section requires.

## What is shared and what is sparse

`app` makes one registry per command and gives it to preparation, the
engine, the Tart provider, and dependent discovery, and closes it with
the store. Staging and discovery both ask for the prepared tree whole,
so a verification with dependents materializes it once. Preparation's
release resolution and its preparation open the same base source, sparse,
and now share that too. The engine's binding of a selection, which every
intake path runs, was a whole-tree materialization before and is sparse
now: a path selector names its directory before resolution, and a reader
materializes what it resolves, which the `Reader` contract now says and
the tests' fake reader honors. The survey ensures the whole tree, since it
resolves names and evaluates what it selected, and hands its projection to
assess and outdated, which adopted the snapshot's directory before.

Sharing changes one thing about a root: two consumers may install the
index into it. Installation copies each index file to a temporary name
beside it and renames, so a reader never sees a truncated index.

A nil registry shares nothing and opens a workspace per acquisition, so
services built outside `app`, in tests among them, need no wiring.

## Measured

The registry's saving is a materialization per consumer that shares one,
which only a verification with dependents exercises in full; the dry-run
path shares its two sparse openings and measures the same three seconds.
The whole-tree materialization the bindings no longer do is the twenty
seconds the [adoption note](2026-09-21-workspace-adoption.md) measured.

## Left of the design

The interposer as a proof, in three parts, and the blob store, each only
if a measurement asks for it.
