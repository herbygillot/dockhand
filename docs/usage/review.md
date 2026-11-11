# Reviewing a pull request

Use `review` when you want another view of a macports-ports pull request: commit
and Portfile findings, upstream changes worth inspecting, and dependent ports
that might need attention. Use adoption when you want a branch to build or edit.
Reviewing does not itself approve or merge the pull request.

## Read the findings without posting

For a pull request numbered 35174:

```sh
dockhand review 35174 --markdown
```

This prints review text for you to read or paste elsewhere and posts nothing to
GitHub. To keep a copy:

```sh
dockhand review 35174 --markdown > review-35174.md
```

The review includes the commit rules Dockhand checks, Portfile findings, and
what the upstream source comparison could establish against the pull request's
base. It can report changed licenses, build files, dependencies, patches that no
longer apply, and dropped patches that still apply. Dependent ports come from
the index at that base: they are candidates to investigate, not a claim that all
need rebuilding.

Reviewing fetches Git objects and records local review history so a later review
can show findings resolved since the previous one. It does not edit the
contributor's working files or create an adopted branch by itself. Read any
notice about an unavailable comparison or an older base; absence of a finding is
useful only for what the review actually checked. Without `--check`, it has not
run `port lint` or built the port.

On a terminal, plain `dockhand review 35174` displays the report and asks
whether to comment, request changes, edit the text first, or post nothing. In a
script, plain review displays and records its report but exits with failure
because no review was posted. Use `--markdown` for a deliberate no-post export;
do not add `--comment` just to make a script's exit status succeed. A script's
review of someone else's pull request is text-only unless it passes
`--evaluate`, described below.

## Decide whether to evaluate the contributor's code

A Portfile is code: MacPorts runs its Tcl as you, on the Mac that holds your
GitHub login. Some review rules and the upstream comparison need the pull
request's Portfiles evaluated, so before evaluating someone else's, review asks:

```
#35174 is @alice's. A full review evaluates its Portfiles with MacPorts on this
Mac, as you. A Portfile is code: it runs with your files and your GitHub login
in reach.
  devel/foo/Portfile                      +12 -3
  _resources/port1.0/group/foo-1.0.tcl    +4 -0
? Evaluate them: y yes · d show the diff first · n review the text only  [n]
```

The list is the Portfiles and PortGroups the pull request changes, since a
changed PortGroup is the author's code too, and `d` shows their diff before you
answer. No, the default, gives a text-only review and isn't kept. Yes is kept
for the pull request and the author's commit it was given for: a new push from
the author asks again, showing what changed since the commit you allowed. Your
own pull requests, and your own commits above an adopted one, ask nothing.

A text-only review runs the commit rules, the Portfile rules that compare text,
and the dependents from the base's index, and reports what it didn't run on its
first line, in the displayed text, the posted text, and `--json`
(`"evaluated": false` and `"left_out"`): restated values, path-style
dependencies, and the upstream comparison. A rule that needs the head evaluated
isn't run, never half-run: a subject isn't judged by the port it names where
the commit changes a port directory, since only the head knows the ports it
defines.

In a script, `--evaluate` answers yes for the head fetched and records it, and
`--evaluate=false` asks for a text-only review, on a terminal too. Without
either, a script's review of someone else's pull request is text-only. `--yes`
never answers this question. `review --check` needs a yes, since planning a check
evaluates the Portfiles on this Mac: a script without `--evaluate` is refused
before anything is adopted, and there is no text-only check.

On an adopted pull request's branch, `check`, `tidy`, `submit`, `diff`, and the
authoring commands ask the same question on a terminal where the author's commit
isn't allowed; a script is refused, naming `dockhand adopt --pr 35174
--evaluate`, which allows it. `status` never asks, and `serve` drives a queued
check of an adopted branch only where its author's commit is allowed; one that
isn't stays queued.

This is consent, not a sandbox. A Tart check isolates the build in a guest, but
its planning still evaluates Portfiles on the host. Dockhand refuses a
local-provider check of an adopted pull request whatever the answer, because
that build would execute the contributor's Portfiles as root in your live
prefix. Choose Tart or GitHub for that branch instead.

## Build it without taking over its publication

To review and build the pull request's committed head in Tart:

```sh
dockhand review 35174 --check
```

Dockhand adopts it as `pr-35174`, or uses its already adopted branch, and
records a check. Tart is selected by default for this command; `--on` can choose
another supported environment. On a terminal it can offer to post the review
afterwards. The posted text names the check and how it ended, in place of the
line saying Dockhand didn't build it. The command returns the check's outcome
whether or not it posts: with `--comment` or `--request-changes`, a failed check
is still posted and the command still exits 2. `--markdown` and `--check` cannot
be combined.

For a workflow in which you inspect the build plan before running it:

```sh
dockhand adopt --pr 35174
cd "$(dockhand path pr-35174)"
dockhand check --on tart --plan
dockhand check --on tart
```

The plan asks before it evaluates the author's Portfiles, on a terminal; a
script adopts with `dockhand adopt --pr 35174 --evaluate` instead.

Use the branch name adoption actually prints if the pull request was already
tracked under another name. You can inspect or edit the adopted files with the
usual branch commands, then `tidy` and check your changes. `review --check`
checks the pull request's committed head; after local edits, run `check` from
the branch's worktree to include those edits. See `dockhand usage checks` for
interpreting the result and `dockhand usage recovery` for failures or
interrupted runs.

`dockhand status -b pr-35174` adds the pull request's merge fitness, as
`dockhand usage publishing` describes it: each port's maintainers, read from
master's port index where dockhand has one cached, whom the description
mentions, who approved, and when MacPorts' 72-hour maintainer timeout ends.
The contributor's Portfiles aren't evaluated only to read them, so a port
whose maintainers neither that index nor a check's change record holds says
`maintainer information not read yet`.

## Post only the review you intend to send

After reading and confirming the findings, these commands explicitly publish a
GitHub review:

```sh
dockhand review 35174 --comment
# Or, when your access permits a formal request for changes:
dockhand review 35174 --request-changes
```

Each command gathers the review again and posts its text, including inline
Portfile comments where applicable. `--request-changes` needs write or triage
access for MacPorts to honor it. Use the interactive edit option or `--markdown`
and your usual GitHub workflow when you need to revise the proposed wording
before sending it. A clean report describes only Dockhand's checks; read the
diff and the build results before drawing your own conclusion.

Adoption grants no permission to push to the contributor's branch.
`submit --plan` shows the proposed destination and any restriction. Dockhand
permits a push there only when the contributor allows maintainer edits and you
have write access to MacPorts' repository. It preserves their description by
default. Renaming the local branch does not change the pull request's source
branch or transfer ownership of it.

Use `dockhand usage branches` to manage the adopted worktree and
`dockhand usage publishing` before submitting changes to someone else's pull
request.
