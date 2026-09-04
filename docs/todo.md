# TODO

Ideas worth doing that are not being done yet, with enough context to
act on later without rediscovering why. Unlike `decisions.md`, nothing
here is ruled; unlike `notes.md`, this is maintained. An entry leaves
when it ships or when it is decided against — and if it is decided
against, it moves to the decision log rather than simply vanishing.

## Promote: say when an open PR already takes this port as far or further

**The ask (maintainer, 2026-09-03).** When promoting, look for an open
pull request on the same port that updates it to the same version or a
newer one, and tell the user. Explicitly **not a hard gate** — a flag,
not a refusal.

**What exists today.** `promote` already searches the open pull
requests on the port (`gh.OpenPortPRs`, now off the search quota and on
paged REST). `verdict.CheckDuplicates` then does two things with the
result:

- An **exact title match**, case- and whitespace-insensitive, is a
  refusal: `DuplicatePRError`, exit 20, overridable with
  `--no-pr-check`.
- Every **other** open PR on the port produces one advisory note:
  *"an open PR already touches this port: #N …"*.

**The gap.** That note is a bare fact about coexistence. It does not
say whether the other PR is doing the same work, less work, or more.
The cases it currently under-serves:

- Someone else has an open PR taking the port to a *newer* version
  than the one being promoted. The promotion is not a duplicate and is
  not wrong, but it is probably wasted, and the user would want to know
  before spending a reviewer's attention.
- Someone else has an open PR at the *same* version whose title is
  worded differently — "bump to 1.9" against "update to 1.9". The exact
  title match does not catch it, so it reads as an unrelated PR.

**The hard part, and why this is not a two-line change.** Getting a
version out of another PR honestly. A title is freeform prose and
parsing one is a guess; a wrong guess here is worse than silence,
because the whole point is to tell the user something true about
someone else's work. Two sounder sources, in order of cost:

1. The PR's changed files — the port's Portfile in its head tree — read
   the way dockhand reads any Portfile. Correct, and expensive.
2. The PR's head branch name, when it was minted by dockhand, since the
   slug carries the version. Free and reliable for dockhand-minted
   branches, useless for hand-made ones.

Whatever the source, the comparison is `macports.VerCmp` and never
string ordering, and the sentence must say where the version came from
so a reader can weigh it. If no version can be established, the honest
output is today's generic note — the feature declines to guess, like
everything else here.

**Where it goes.** `verdict.CheckDuplicates` is the pure judgment and
already returns notes plus an optional refusal; this is a richer note,
not a new gate, so its shape does not change. The version facts would
have to arrive on `PRFact`, gathered at the engine boundary where the
rest of the GitHub mapping happens, so `verdict` stays pure.

**Not to be confused with** the same-port supersede rule, which is
about *this* checkout's own sibling branches and is already ruled and
built. This one is about other people's work.
