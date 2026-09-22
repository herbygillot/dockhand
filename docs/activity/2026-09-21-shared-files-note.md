# 2026-09-21: a changed shared file is named beside the verdict

## Why

The scope rule of the [previous note](2026-09-21-shared-resources-in-scope.md)
lets a contribution change a PortGroup beside its port, and verification
builds the port only. A green build then implies more than it proves. The
reviewer's first question would be which ports load the group; dockhand
now answers it before it is asked, as a note rather than a refusal.

## What changed

- `record.SharedFile` is a file under `_resources` a revision changes, with
  the paths that load it: Portfiles for ports and group files for other
  PortGroups. `record.Revision.Shared` lists them, persisted in a new
  `shared` column by migration 024.
- `changeset.SharedUsers` finds them: the paths changed from the base,
  those under `_resources`, and for each PortGroup file, named
  `<name>-<version>.tcl`, a search of the tree for its `PortGroup` line in
  every Portfile and group file, through the new `git.GrepTree`. A shared
  file that is not a group, a livecheck or fetch definition, lists no
  loaders, since every port's evaluation reads it.
- The engine records the list at every place a revision is made: branch
  adoption at submission, the two adopt paths, preparation's integration,
  and reassociation. A failed search is reported at the verbose level and
  the revision is recorded without it.
- The pull request body gains a "Shared files" section ahead of "Tested
  on"; `status <port>` lists each shared file with its loaders; the live
  table's expansion carries the same words. The wording is one place,
  `view.SharedWords`: "changes PortGroup java-1.0, loaded by 144 ports and
  2 other PortGroups; only Okapi was built".

## Evidence

The changeset test builds a tree with a group loaded by one port and one
other group, a port on another version of the group, and a livecheck
file, and checks the loaders and the group-name parsing; the wording test
covers ports, other groups, and non-group files; the body test checks the
section and its place; the workflow, store, and publish suites pass.
