# 2026-09-22: a main port authorizes the subports sharing its release

## What was decided

`dockhand bump atuin` stopped at "shared release also changes
atuin-server; authorize with bump --shared-release". The rule existed for
a named subport: `bump py313-ipdb` must not silently move py312-ipdb. A
main port is a different case. Its Portfile is the release: a subport
that inherits the main port's `version`, as atuin-server inherits atuin's
`github.setup`, moves with it by construction, and the person reviews the
diff that shows both. Asking for a flag there is friction with no
decision behind it. The user decided: acceptable, and the default.

## The change

`portedit` authorizes the shared release when the resolved selection is a
main port, after stub resolution, in the same place a stub's carrier is
authorized, and the workflow records the same authorization on the job
when it binds a bump, beside the stub's, so the gate that refuses an
unapproved release scope reads it from the job as before. A named subport
still needs `--shared-release`, and the refusal names the sibling that
would move, which for a subport selection may now be the main port
itself. A release scope is recorded only when it holds more than the
target; a plain bump of a port with no sharing sibling records none,
authorized or not, so nothing downstream sees a cohort of one.
`ReleaseScope` is unchanged: protected members, those pinning their own
version, stay protected either way.

The first live check found a second problem, a runaway overlay cost that
the [overlay note](2026-09-22-overlay-scope.md) covers.

The dependency test's unauthorized scenario now selects the subport, so it
keeps testing the refusal, and the portedit scope test already selected
one. `docs/cli-design.md` and `docs/usage.md` say which selections need
the flag.

## Excluding a subport

An `--exclude <subport>` flag was considered and dropped in the same
conversation: a subport that shares the main port's version has no
version of its own to hold back, so excluding it would mean writing a pin
into the Portfile, which is a maintainer's edit, not a bump option. A
subport that must stay behind pins its version already, and
`ReleaseScope` protects it.
