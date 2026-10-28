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
do not add `--comment` just to make a script's exit status succeed.

## Decide whether to evaluate the contributor's code

Some review rules and the upstream assessment evaluate the pull request's
Portfiles with MacPorts on this Mac, as your user. Dockhand identifies whose
Portfiles it is evaluating. A Portfile is executable Tcl; reviewing with
`--markdown` does not turn that evaluation into an isolated or text-only
analysis. Read the proposed files first through GitHub or another viewer if you
have not decided to run their code on your Mac.

A Tart check isolates the build in a guest, but its planning still evaluates
Portfiles on the host. Dockhand refuses a local-provider check of an adopted
pull request because that build would execute the contributor's Portfiles as
root in your live prefix. Choose Tart or GitHub for that branch instead.

## Build it without taking over its publication

To review and build the pull request's committed head in Tart:

```sh
dockhand review 35174 --check
```

Dockhand adopts it as `pr-35174`, or uses its already adopted branch, and
records a check. Tart is selected by default for this command; `--on` can choose
another supported environment. On a terminal it can offer to post the review
afterwards. In a script, with no posting flag, it posts nothing and returns the
check's outcome. `--markdown` and `--check` cannot be combined.

For a workflow in which you inspect the build plan before running it:

```sh
dockhand adopt --pr 35174
cd "$(dockhand path pr-35174)"
dockhand check --on tart --plan
dockhand check --on tart
```

Use the branch name adoption actually prints if the pull request was already
tracked under another name. You can inspect or edit the adopted files with the
usual branch commands, then `tidy` and check your changes. `review --check`
checks the pull request's committed head; after local edits, run `check` from
the branch's worktree to include those edits. See `dockhand usage checks` for
interpreting the result and `dockhand usage recovery` for failures or
interrupted runs.

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
