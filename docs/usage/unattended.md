# Unattended work

`bump`, `ship --yes`, and `serve` carry updates to their pull requests with nobody looking along the way, so they hold where a person should look, and `dockhand decide` answers a hold.

## bump

```sh
dockhand bump jq           # the newest release, all the way to its pull request
dockhand bump jq 1.8.1     # a version you name
```

`bump` runs `ship <port> --yes`, optionally targeting a version you specify. It submits the update without prompting at each stage; `ship` asks once, and `update` is the edit alone. It isn't `port bump`, which refreshes checksums, as `checksums` does. It updates the port on fresh master, in a branch it starts for the edit, or resumes the open branch already changing it, tidies the edit into one commit, checks it, and submits exactly that commit once the check passes. Its `--json` result is `ship`'s, and `--plan` shows what it would do, as `ship --plan` does, changing nothing. It takes the flags `ship` takes for one port: `update`'s edit flags, such as `--revbump-dependents`, `--except`, `--shared-release`, `--keep-old-checksums`, `--with-obsolete`, `--follow-moves`, and `--closes`; `check`'s `--build`, `--on`, and `--tests`; and `submit`'s `--type`, `--draft`, `--tested-binaries`, `--tested-variants`, `--title`, `--note`, and `--skip-notification`.

It stops wherever a person should look:

- Before it edits anything, it stops with nothing changed when the check has nowhere to build, or the port is already at the release. Where it can't tell which release is the newest, as `update` can't, it exits 3 and names the `bump` that takes the one set aside.
- Once it knows the release, and before it downloads or builds anything, it looks for other open pull requests for the port. One found, or not knowing, would hold the submission after the check, so it stops there, changing nothing: it exits 3, as a held check does, and names the `ship <port>` that goes ahead after a look.
- A failed check leaves the branch, with its logs.
- A passing check is held, as `serve`'s are, when the upstream comparison found something a build can't catch or couldn't compare the archives, a commit rule finds an error (a warning is said and holds nothing, as `tidy` keeps the commit), another pull request is open for the port, or couldn't be looked for, the port's major version moves (`holds.majors`), its GitHub repository moved or has a new owner, or the revision raises the oldest macOS it builds on, its platforms' darwin floor or its `macosx_deployment_target`: it looks again before submitting, since one may have opened while it checked. `bump` exits 3 and names the `submit` that finishes it after a look; with `--json`, `submit.held` lists why.

The pull request's tested checkboxes stay unticked unless `--tested-binaries` or `--tested-variants` says otherwise: they say what you tested, which dockhand can't. The one exception is a passing `check --variants each` of the port, which ticks the variants item by itself and says which variants it built, since it built them.

## Answering a hold

What holds a submission nobody looks over, an upstream finding or a major version, is yours to answer with `dockhand decide <rule> [<port>]`, the rule's name as status and the preview give it, which they name beside each hold:

```sh
dockhand decide license-changed -p jq --because "the new license is still free, and the Portfile says GPL-3"
dockhand decide license-changed -p jq --waiting "upstream's word on the COPYING change"
```

`--because` takes it as acceptable: serve, ship, and bump no longer wait on it, status and the preview still say it, and the description gives your reason under "Reviewed:", quoted as your note is. `--waiting` leaves it for later, and submit refuses the branch until you decide it `--because`. A decision rests on the port's own files, its Portfile and `files/`, so it lapses when they change, as with the port's next version, and stands when another port of the branch changes. Deciding again replaces it.

## serve

```sh
dockhand serve               # in a terminal of its own
dockhand serve --install     # as a launchd agent that starts at login
dockhand serve --drain       # run what is queued now, then exit, naming the open pull requests it leaves unwatched
```

`serve` runs queued checks, people's before its own, and keeps running new ones as they are queued. One serve leads. A second stands by and takes over if the leader dies. Stopping serve leaves the check it was running for the next serve, which picks it up where it stopped.

Between checks it:

- reads your open pull requests every few minutes, so `status` shows their reviews and CI, and marks a merged one's branch merged;
- once a day, at `serve.outdated_at`, looks for new releases of your ports, or the ports `serve.maintainers`, `serve.categories`, and `serve.not_maintainers` select as `outdated`'s flags do, and carries them as far as `serve.ship_to` says, under the same holds and `serve.submit_limit` either way, as `ship --to` does: `list` counts them for status, `commit` prepares a branch with one commit for each, `check` also checks each, on `build.unattended`'s profile, and `submit` also submits each that passes. A port that may have one, as `outdated` says, is never prepared: serve and status list it for your look. Without a `maintainer` setting it can't know your ports, and says the line to set, as `create` does: the one master's ports write your GitHub login with;
- once a day, unless `cleanup.automatic = false`, cleans up automatically, as below;
- posts macOS notifications as checks finish and pull requests change, and when the day's look finds a different set of your ports with newer releases than the last look did: "2 ports have newer releases (dockhand outdated --mine)". They are posted through AppleScript, so macOS credits them to Script Editor, and clicking one opens it. `serve.notify = false` turns them off, and `--no-notify` turns them off for one run.

Serve opens no pull requests by default. With `serve.ship_to = "submit"`, it opens one for each branch it prepared whose check passed, at most `serve.submit_limit` a day. It never opens one with an upstream finding or a commit-rule error, one whose archives couldn't be compared, one needing `--accept`, or one for a port another open pull request updates; those wait on the attention list, and the pull request says serve opened it without a person's review. Use `--ship-to` to set the stage for one service run. For example, `serve --ship-to check` prepares and checks updates without submitting them.

`--install` runs the agent with the flags given beside it, such as `--no-notify`, and with the settings serve was installed under: `--git`, `--db`, and the environment variables below that serve reads, each path made absolute. Run it again after changing them. `--uninstall` removes it. Both wait until launchd has let go of the agent, which it does a few seconds after it's told to, up to thirty seconds, and say so where it hasn't.
