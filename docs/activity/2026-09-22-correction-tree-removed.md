# 2026-09-22: the correction request's prepared tree is gone

`workflow.CorrectionRequest.Tree` let a caller hand `BindCorrection` a
prepared tree to commit in place of a checkout capture. Its one caller
was `app.prepareOnto`, which prepared an update outside the durable job
and translated the result into an amendment. The [one owner for
preparation](2026-09-22-one-owner-for-preparation.md) moved that update
into the workflow as an `Onto` resolution, and `prepareOnto` went with
it; nothing set the field after that, in production or in tests, while
`BindCorrection` still validated it and let it replace the snapshot.

The field and its branch are removed. The squash case that followed it
is now a condition of the checkout capture rather than an empty branch:
an amend of the current branch takes the checkout unless it squashes,
and a squash takes the branch's tree as committed. Behavior is otherwise
unchanged.

## Validation

`go build ./...` and `go vet` pass; `app` and `cli` tests pass. Five
`workflow` tests fail in the cloud container this was run in, and fail
identically without the change: the two merge-cleanup tests make the
fork's refs directory read-only to force a failed deletion, which root
ignores, and the three squash tests commit with Git's own identity under
`GIT_CONFIG_GLOBAL=/dev/null`, which the container's hostname cannot
supply.
