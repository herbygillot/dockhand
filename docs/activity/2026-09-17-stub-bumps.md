# Bumping a stub bumps its subports

The whole-tree survey counted 2,256 `py-*` stubs as unsupported (the python PortGroup replaces `fetch` for a port with no distfile of its own), and the ports tree's history shows why that mattered more than the count: 2,324 of last year's 2,440 python bump commits are written as `py-foo: update to X`. Maintainers name the stub. dockhand refused it.

## The rule

`macports.StubMembers` recognizes a stub from an evaluated Portfile: a port that is metadata-only while sibling subports at the same version are not. The newest member by natural name order (`py314-foo` after `py39-foo`) is the edit target. Two places apply it, so previews and jobs agree: `portedit.load` redirects the selection before any probe or edit, marks the request as a shared release, and keeps the stub's name for the commit subject; `workflow.BindPreparation` does the same for the job, recording `Preparation.Stub` so the contribution's initiating target, its branch name, and later `status`, `verify`, and `publish` all use the person's name for the port. A retry carries the stub name from the prior job.

## What gets built

Decided with the user: two local VMs make five subport builds slow, and the pull request's own CI expands every changed Portfile with `mpbb list-subports` and builds every subport anyway. `ReleaseScope.RequiredTargets` therefore plans the initiating target alone unless the job's `AllSubports` is set, which `--all-subports` sets on `bump` and `verify`. Publication coverage follows the same rule, and the PR body's shared-release section names what was not built locally and says the workflow builds every subport. A job asked for all subports keeps requiring them, so a failed sibling still gates that job; a later root-only verification is the default coverage.

The existing shared-release tests that assumed every sibling was built now set `AllSubports`; new tests cover stub detection and ordering, the binding redirect, the required-target rule, and the summary wording. A live preview of `bump py-urllib3 2.8.0 --diff` on the ports tree exercised the real python PortGroup.

## Found by the real run

The first real `bump py-idna` stopped at release resolution: the job's preparation request selected by its recorded target, `py314-idna`, so the edit service never saw the stub and the subport's own livecheck is `none`. The preparation request now selects the stub's Portfile again whenever the job records a stub, so the edit service redirects to the same newest subport and borrows the stub's livecheck. The preview had not caught it because the CLI passes the stub's name to the preview directly.

The third run completed the exercise: `bump py-idna` continued the contribution, reused the passing `py314-idna` build, and published https://github.com/macports/macports-ports/pull/34737, whose body lists the one local build and names py310 through py313 as left to the workflow. The second run had stopped at publication with "missing shared-release target py310-idna", which neither the recorded data nor the current code reproduces; a combined-bump test with root-only coverage now pins that path.
