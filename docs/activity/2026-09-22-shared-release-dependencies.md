# 2026-09-22: a shared release survives dependency regeneration

## What was reported

`dockhand bump atuin` needed attention: "shared release also changes
atuin-server; authorize with bump --shared-release". That is the rule as
designed, since atuin's Portfile has a buildable subport sharing its
`github.setup` version. But the authorized form failed too, with a list
of atuin-server options that had changed: version, its followers, and
`cargo.crates`. Three binaries agreed, including one built at 06:00 the
same day before any change: not a regression, a defect in the path a
Rust or Go port takes.

## The defect

`prepareDependencyVersion` regenerates the dependency block and then
checks that no sibling changed, comparing every non-target port of the
family before against its state after, exactly. A sibling that shares the
release moves by design: its version, the options that follow the version,
its checksums, and the regenerated block, which is one declaration for the
whole Portfile. The version check before the downloads knew that through
the release scope; the dependency check did not, so every shared-release
bump of a Rust or Go port failed after the archives were downloaded.
`assess --shared-release --at` passed, since it stops before the archives.

## The fix

The dependency check computes the release scope the same way, which also
refuses an unauthorized move with the same message, and for an affected
member expects what a version bump expects: the version and its followers,
the checksums, and the generated dependency keys taken from the after
state, the revision free to reset; every other sibling must be untouched.
`bump atuin --shared-release --dry-run` prepares the edit and lists both
members.

## Evidence

The cargo preparation test gains a subport sharing the version: authorized,
the preparation succeeds with two affected members; unauthorized, it is
refused with the shared-release message.

## A question left open

`bump atuin` without the flag is refused by the rule that a shared release
needs authorization, written for a named subport moving its siblings. For
a main port whose subports share its version by construction, every
maintainer commit moves them together, and the refusal asks the user to
say what the Portfile already says. Whether a main-port selection should
authorize its own subports is a policy decision, recorded here rather than
made.
