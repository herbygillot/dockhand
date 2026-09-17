# Bumping a stub bumps its subports

The whole-tree survey counted 2,256 `py-*` stubs as unsupported (the python PortGroup replaces `fetch` for a port with no distfile of its own), and the ports tree's history shows why that mattered more than the count: 2,324 of last year's 2,440 python bump commits are written as `py-foo: update to X`. Maintainers name the stub. dockhand refused it.

## The rule

`macports.StubMembers` recognizes a stub from an evaluated Portfile: a port that is metadata-only while sibling subports at the same version are not. The newest member by natural name order (`py314-foo` after `py39-foo`) is the edit target. Two places apply it, so previews and jobs agree: `portedit.load` redirects the selection before any probe or edit, marks the request as a shared release, and keeps the stub's name for the commit subject; `workflow.BindPreparation` does the same for the job, recording `Preparation.Stub` so the contribution's initiating target, its branch name, and later `status`, `verify`, and `publish` all use the person's name for the port. A retry carries the stub name from the prior job.

## What gets built

Decided with the user: two local VMs make five subport builds slow, and the pull request's own CI expands every changed Portfile with `mpbb list-subports` and builds every subport anyway. `ReleaseScope.RequiredTargets` therefore plans the initiating target alone unless the job's `AllSubports` is set, which `--all-subports` sets on `bump` and `verify`. Publication coverage follows the same rule, and the PR body's shared-release section names what was not built locally and says the workflow builds every subport. A job asked for all subports keeps requiring them, so a failed sibling still gates that job; a later root-only verification is the default coverage.

The existing shared-release tests that assumed every sibling was built now set `AllSubports`; new tests cover stub detection and ordering, the binding redirect, the required-target rule, and the summary wording. A live preview of `bump py-urllib3 2.8.0 --diff` on the ports tree exercised the real python PortGroup.
