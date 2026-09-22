# 2026-09-22: workflow tests that do not lean on the host

Five `workflow` tests failed in a cloud container and passed on a
developer's Mac, for two reasons that had nothing to do with the code
under test.

**A guessed author.** The squash tests reach `Repo.Author`, which asks
Git for `GIT_AUTHOR_IDENT`. `TestMain` points `GIT_CONFIG_GLOBAL` at
`/dev/null`, so the fixture repositories had no configured identity and
Git guessed one from the host; a hostname that yields no email, such as
`vm.(none)`, makes the guess fail. The base fixture now names an
identity in the repository's own configuration, as the correction
fixture already did for itself, so no fixture depends on the host.

**A refusal root ignores.** The two merge-cleanup tests forced a failed
fork-branch deletion by making the bare fork's `refs/heads` read-only.
Root ignores permission bits, so the deletion succeeded. They now
install a `pre-receive` hook in the fork that refuses deletions and
remove it to let the retry through, which fails the push the same way
for any user.

## Validation

The `workflow` package passes as root and as an unprivileged user in
the container. Three tests elsewhere still fail there, unchanged by
this: `macports/eval` wants `tclsh`, `tart/provision` wants `/bin/zsh`,
and `patchcheck` expects the BSD `patch` wording, "hunks failed", where
GNU `patch` says "hunk FAILED". Each is a macOS assumption rather than a
root one.
