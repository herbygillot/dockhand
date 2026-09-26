# 2026-09-26: a unique ID for every provider run

The person asked for a unique ID for every provider run: to mark the run in
the pull request, and to look up its evidence by, such as its logs. For
GitHub, the workflow run's URL serves. For the prefix provider, the same
model should hold.

## The model

A provider run is a `GuestExecution`: one attempt at a check's targets in
one environment. It had an ID already, random and unique (`ex_…`, 80
bits), but it was internal and never shown.

- **Named for its provider.** A new run's ID is `tart_7y62p4sigena6xlr`,
  `github_…`, or `command_…`. It is unique across machines and databases,
  and says what it is in a pull request. Older runs keep their `ex_` IDs.
- **The provider's own reference.** `GuestExecution.ProviderRef` was meant
  for the provider's own name for a run, but nothing set it. A new
  `Build.Refer` records it:
  - Tart's is the VM clone's name;
  - GitHub's is the workflow run's URL;
  - the command provider's is a `reference` its script may write in the
    result file.

  The runner now ends an execution from the build's copy, so a reference,
  or an observed environment, is never written over.
- **The prefix provider** isn't in v3 yet. The model is the provider's
  alone to fill, so its runs will get IDs, and whatever reference it
  records, like the others.

## Where it shows

- **The check's progress:** "check-9: attempt 1 of 3 on tart macOS 26
  (Tahoe) arm64 with Xcode, run tart_7y62p4sigena6xlr".
- **The pull request.** Tested on names the runs behind each
  environment's results, after who built them: "Xcode 26.6 17F113 · tart:
  built in a clean VM, run tart_7y62p4sigena6xlr".
  - A reference that is an `https://` link anyone can follow, such as
    GitHub's workflow run, is named instead of dockhand's ID.
  - Results from two attempts, or from an earlier check of the same
    files, name each run.
  - The runs come from the results themselves, since each result records
    its execution, and so does what Tested on says the environment
    reported.
- **`dockhand logs`** takes a provider run's ID or its provider's
  reference, as well as `check-42`, and then shows that run alone.
  `--port` prints one port's log from it.
  - Its listing shows each run's ID, and the reference beneath.
  - A name shaped like a check keeps "there is no run check-9".
- **The command provider's request** carries the run's ID as `execution`,
  for the script to label its own logs with.
- **`--json`**: `logs` gives each run's `id` and `reference`.

## Store

- `store.Reader` gained `Execution(id)` and `ExecutionsReferred(ref)`.
- A reference may repeat: a GitHub run that is run again is the same
  URL. So the latest run with it is the one found.

## Tests

- **Tested on:** a run's ID, GitHub's URL instead, and two attempts' runs.
- **The submit test's body** names its `tart_` run.
- **Tart and GitHub** each report their reference: the clone's name, and
  the run's URL.
- **`logs`:**
  - by a command run's ID, and its port's log;
  - by GitHub's run URL;
  - a name that is neither a check nor a provider run.
- **The command provider:** the request's `execution`, and a result's
  `reference` finding its run.

## Then: the person's wording

The person settled how a run reads in the pull request:

- **Each run names its check.** "Xcode 26.6 17F113 · tart: built in a
  clean VM (Run ID: tart_b7x7ddf53ukr6wbn - checked in check-11)". Runs
  from two attempts or checks read "(Run IDs: … - checked in check-10;
  … - checked in check-11)".
- **"Checked by dockhand check-N" goes.** Each run line names its check
  now, the earlier checks' included.
- **A last line** closes the description: "Submitted by
  [dockhand](https://github.com/herbygillot/dockhand) ver. <version>".
  - The version is the trailer form commit messages already use
    (`version.Current().Tag()`).
  - The link is written `[dockhand](url)`, since the `(dockhand)[url]`
    asked for would show as brackets on GitHub.
  - It sits in the part of the description dockhand refreshes, so a later
    submit updates the version.
- **It renders with anything missing.** The person asked that the
  description still render when this information is missing, whatever the
  reason. Each gap has its own test:
  - a macOS version without its build, or Xcode without its build, drops
    only what's missing;
  - an unreported environment is named by its release and stated tools;
  - a run whose check isn't known gives its ID alone, and no runs leave
    the parentheses out;
  - a build that doesn't know its version signs without "ver.".
