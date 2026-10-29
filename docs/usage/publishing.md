# From a working change to its pull request

Use this page when the port's files are ready and you want to shape the commits,
understand what was checked, and publish or update a pull request. It also
explains the different meanings of a failed-build acceptance, an upstream
concern, and a draft.

The examples assume a tracked branch named `jq-update` in your ports checkout.
Substitute the name dockhand reports for your change. Run
`dockhand usage branches` if you need to find or adopt it, and `dockhand usage`
for initial setup, a fork remote, or a GitHub login. Every example uses `-b` so
it works from the main ports checkout as well as the branch's worktree.

## The deliberate path

```sh
dockhand status -b jq-update
dockhand diff -b jq-update
dockhand tidy -b jq-update --plan
dockhand tidy -b jq-update
dockhand check -b jq-update --head
dockhand submit -b jq-update --plan -v
dockhand submit -b jq-update
```

Read each result before taking the next step. Tidy prepares local commits. Check
builds the committed files in this example. Submit's preview explains what would
be pushed, where, as which account, with what evidence and description. The
final command pushes and opens or updates the pull request.

This sequence gives you separate places to inspect the files, history, build
results, and publication. Dockhand can also carry an update through the stages:
`dockhand ship jq` or `dockhand bump jq`. Read `dockhand usage unattended` for
the holds and resumption rules of that path.

## Shape the commits a reviewer will read

Tidy considers all the branch's changes, including uncommitted edits. By default
it proposes one commit per port directory; when dockhand made several successive
updates within one Portfile and the recorded edit chain still stands, it can
preserve a commit for each update. It uses the subjects its authoring commands
recorded or the messages in your existing commits.

```sh
dockhand tidy -b jq-update --plan
```

This shows the proposal without applying it. Read the grouping, subjects,
findings, and any explanation that a change needs your review. A finding's
bracketed code can be explained with `dockhand explain <code>` using the code
printed in your output.

Tidy usually preserves the final files, but it also fixes deterministic lint
findings on lines your branch changes and reports those fixes. Check makes the
same fixes before capturing files. Whole-file formatting is separate:
`tidy --restyle` makes a final whitespace commit for the Portfiles it can safely
restyle. Keep that choice out of a change when you only want its functional
edits.

On a terminal, tidy lets you review the diff, rearrange groups, edit subjects,
and apply or cancel. For one commit with a message you supply:

```sh
dockhand tidy -b jq-update --squash --message "jq: update to 1.8.1"
```

Use a subject that describes your actual change. `--group "2 1+3"` means
proposed commit 2 first, then one commit combining 1 and 3. Numbers refer to the
current proposal, not Git commit IDs. If you combine work by different authors,
`--author "Name <email>"` supplies the attribution for the combined commit.

For a reviewable file you can inspect outside the terminal menu:

```sh
dockhand tidy -b jq-update --plan --out jq-plan.toml
# Review the TOML and edit the proposed messages if needed.
dockhand tidy --apply jq-plan.toml
```

The saved plan identifies the branch and binds its base, commits, and files.
Applying it refuses if those changed meanwhile; generate a new plan instead of
treating an old file as current. `--restyle` is not saved with this form: apply
the plan without it and run restyle separately if wanted.

Without a terminal, a plan containing only dockhand's own unambiguous edits can
apply without asking. Standalone `tidy --yes` explicitly applies the displayed
proposal, including a proposal involving hand edits. The implicit tidy stage in
a nonterminal `ship` or `submit` is more limited: their consent to proceed does
not approve an unseen hand-edit commit plan. Run tidy explicitly when that stage
asks for review.

Tidy keeps a checkpoint before changing history, and retains an existing commit
when there is nothing to rewrite about it. An existing pull request does not
change until you submit. `dockhand usage branches` explains `undo` and what it
restores.

## Know which files the evidence belongs to

A check builds files, not a commit message. A passing snapshot can therefore
cover the commits tidy makes if their files and required inputs still match.
Conversely, having a green check somewhere on the branch is insufficient if you
edited files afterward, changed an environment, or left a required target
unbuilt.

In the recipe above, `--head` makes the choice unambiguous: check the committed
tip. For checking work before committing, read `dockhand usage checks`; captures
of working files, the index, and untracked files have different uses.

A narrowed check adds evidence for the targets and environments it actually
built. It does not erase the need to check the other changed targets. Submission
combines applicable results for the same files and evaluates the required
environments: those the newest check was asked to build on with `--on` or
`--build`, or, where it named none, its own and `build.default`'s. The preview names missing, outdated, or failed results instead of
assuming a narrowed pass covers the whole change. Build-environment identity and
fetched Git commits can also make an earlier result no longer stand. Recheck
when the output asks for it; reuse can spare the builds whose inputs remain
unchanged.

`ship` binds the commit it prepared to the check it carries through to
submission. If that branch moves while the check runs, it stops rather than
publishing a different commit. A standalone `submit --plan` is a preview of the
state it reads; a later `submit` constructs its plan again. It is not a saved
authorization token or a promise to publish files you subsequently changed.

## Read the publication preview

```sh
dockhand submit -b jq-update --plan -v
```

Check four things in its output:

1. **Destination:** the branch, commits, fork, GitHub account, and existing pull
   request if any. Where several remotes could be the destination, choose one
   with `--remote <remote-name>`.
2. **Evidence:** what built, where, with what test policy, and anything still
   missing or accepted. A local pass and MacPorts' later pull request CI are
   separate observations.
3. **Review findings:** commit rules, what upstream changed, other pull
   requests, and concerns a passing build cannot settle. `!` identifies
   something requiring review on an unattended path; `·` is informational.
4. **Title and description:** what a reviewer will actually receive, including
   limitations. `-v` shows the full proposed description where the ordinary
   preview shortens it.

A preview can read upstream information and fetch archives to assess the change.
It does not make the proposed commits, push, or publish a pull request. If your
edits are uncommitted, `submit --plan` first shows the tidy proposal and stops
there. Commit the chosen plan, then preview submission again to see the actual
destination and description.

Ordinary `submit` also offers to make the tidy commits first where it encounters
uncommitted work. To publish the existing committed tip and deliberately leave
local edits out, use `submit --head`; the preview lists what is left out. This
is useful when a later experiment is in the worktree and should not enter the
pull request.

## Publish, and say what you personally tested

```sh
dockhand submit -b jq-update
```

At a terminal, submit offers a description preview or edit and asks for the
final submission. Without a terminal, `--yes` is required. A direct submission
shows concerns for you to weigh; the automatic path used by bump and serve holds
on those concerns. Structural blocks, such as missing required evidence, someone
else's intervening push, or an explicit waiting decision, still need resolving.

The command pushes your committed branch to the selected fork and opens a pull
request against `macports/macports-ports`, or updates the pull request already
associated with it. It does not merge it. When a rewritten history needs
publishing, the push is conditional on the remote branch still being where
dockhand observed it, so someone else's newer push is not overwritten.

Use `--note` for context the generated evidence cannot express:

```sh
dockhand submit -b jq-update \
  --note "Tested the command against our existing query corpus."
```

The note stays with the branch for later submissions. A new `--note` replaces
it; `--note ""` clears it. `--title` explicitly sets the title, including an
existing pull request's title. Without it, an existing pull request is not
retitled just because a commit subject changed.

`--tested-binaries` and `--tested-variants` state that you made the
corresponding manual checks; do not infer them from an install alone. A passing
`check --variants each` can answer the variants item from what it actually
built. The generated Tested on section describes recorded environments and
outcomes and gives the check/provider references a reviewer can follow. A
reviewer cannot read logs stored on your Mac, so keep the explanation of any
accepted failure useful in the description or note.

Descriptions edited on GitHub are preserved where they are no longer dockhand's
unchanged text. Subsequent submissions refresh the sections dockhand still owns.
`--rewrite-description` explicitly replaces the full description with dockhand's
version, preserving your stored note; inspect that preview before choosing it.
For someone else's adopted pull request, their description stays theirs.

If the command ends during publication, first inspect
`dockhand status --refresh -b jq-update`, then rerun submit if needed. Dockhand
looks for the pull request already opened from its branch and can finish
recording an operation whose reply was lost. Do not create a replacement branch
merely because the final confirmation did not print.

## Choose the right way through an incomplete or failed check

These choices answer different questions:

- `submit --draft`: Share work while checks are incomplete or failing. The pull
  request is a draft and shows its evidence. One already open and ready for
  review is returned to draft before anything is pushed to it; where GitHub
  refuses that, nothing is pushed, and `gh pr ready <number> --undo` does it.
- `submit --no-check`: Publish without a check. The description explicitly says
  no local build ran.
- `submit --accept <port>`: Accept an eligible recorded failure, or a new port's
  lint warnings, for this commit.
- `submit --accept <port> --standing`: Accept a specifically identified
  dependency-install failure across branches until its dependency version or
  image changes.
- `decide <rule> ... --because ...`: Record why an automatic-submission concern
  is acceptable for these port files. It is not build evidence.

For a failing changed port, an ordinary `--accept` needs a baseline reproducing
the same failure at the same phase in the relevant environments. A failed extra
from `--also` or a revision-only target can be accepted without establishing
that baseline; the pull request says the cause was not established. A port that
was never built has no build failure to accept. Read the exact refusal rather
than using acceptance to stand in for missing required coverage.

A baseline is the base of the check you are explaining, not whatever master
happens to be now:

```sh
dockhand check -b jq-update --baseline
dockhand submit -b jq-update --accept jq --plan
```

Inspect the comparison before applying the same acceptance in an actual
submission. `--standing` has a narrower meaning: every relevant failure must
identify a dependency that failed to install, its version, and the environment
identity. It cannot accept an arbitrary recurring compiler or test failure. The
output explains eligibility and records what was accepted.

For a concern such as an upstream license change, use its printed rule and a
reason:

```sh
dockhand decide license-changed -p jq \
  --because "The new license is acceptable and the Portfile declares it."
```

Or record what must happen before proceeding with
`--waiting "upstream's clarification"`. That waiting decision blocks ordinary
submission until replaced. Decisions lapse when the port's own files change;
editing a different port does not by itself lapse them. A concern decision does
not accept a failed build, and a failure acceptance does not explain a license
or source change. `dockhand usage unattended` develops these decisions;
`dockhand usage recovery` helps diagnose the failure first.

To share incomplete work and later request review:

```sh
dockhand submit -b jq-update --draft
# Make the remaining changes, then tidy and check them.
dockhand submit -b jq-update --ready
```

`--draft` and `--ready` are separate invocations. Ready submission requires the
evidence ordinary submit requires. If GitHub's organization policy refuses
dockhand's app, dockhand can use an installed GitHub CLI signed in as the same
account to mark it ready, or prints the remaining action when it cannot.

## Continue after a reviewer responds

```sh
dockhand status --refresh -b jq-update
dockhand open -b jq-update
```

Refresh reads current review and CI observations; open shows the pull request in
the browser. Edit the same tracked branch, inspect the diff, tidy and check the
new work, and submit again. The existing pull request is updated. Where a
reviewer requested changes, submit can ask to request their review again after
pushing. `submit.rerequest_review` controls that behavior; see
`dockhand usage configuration`.

If the branch needs a newer master, rebase it, follow status's check advice, and
submit the result. A check on your machine is distinct from the pull request's
MacPorts CI; `retry --ci` reruns eligible failed GitHub jobs and requires the
necessary repository access. Diagnose it with `dockhand usage recovery` rather
than rerunning a local check to try to change GitHub's CI status.

After merge, use `dockhand status --refresh`, then `dockhand clean --plan`.
Cleaning, archiving unfinished work, and starting a follow-up from commits past
a merge are covered in `dockhand usage branches`.
