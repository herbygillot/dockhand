# The command provider

`check` can build on your own script: a build box, a VM you manage, or anything else. Dockhand gives the script a request file and reads back a result file. It cannot vouch for how the script built, so the results read "reported by <name>" (Design v3 §7).

## Setting it up

In `~/.dockhand/config.toml`:

```toml
[providers.command]
run = "~/bin/build-ports"   # run by sh, with the request file's path as $1
name = "my build box"       # labels its results; "command" when unset

[check]
on = ["command"]            # the default for check; --on command also selects it
```

`check` runs the command once for each attempt. Trouble with the environment is tried again, up to three attempts in all, and a verdict is never repeated. Examples of such trouble: the command exits without writing a result file, or writes one that can't be read.

## The request file

The file is `request.json` in a directory of its own. Its path is also given as `$DOCKHAND_REQUEST`. The command runs in that directory, and its output goes to `command.log` there.

```json
{
  "version": 1,
  "run": "check-3",
  "attempt": 1,
  "bundle": "/…/logs/check-3/command-1/source.bundle",
  "ref": "refs/dockhand/check/check-3",
  "commit": "7e3f1a2…",
  "base": "4c1e2d0…",
  "platform": {"OS": "", "Version": "", "Architecture": ""},
  "tests": "declared",
  "targets": [
    {"id": "libharbor", "name": "libharbor", "portfile": "devel/libharbor/Portfile", "kind": "substantive", "role": "changed"},
    {"id": "harbor-cli", "name": "harbor-cli", "portfile": "devel/harbor-cli/Portfile", "kind": "revision-only", "role": "changed", "depends_on": ["libharbor"]}
  ],
  "result": "/…/logs/check-3/command-1/result.json",
  "logs": "/…/logs/check-3/command-1"
}
```

- **`bundle`** holds `commit` under `ref`, less what `base` already holds. `base` is a commit of MacPorts' `master`. Fetch it, or have a clone recent enough to contain it, and then fetch the bundle:

  ```sh
  git -C ports fetch "$bundle" "$ref"   # ports is a clone of macports-ports
  git -C ports checkout --detach "$commit"
  ```

  For uncommitted work, `commit` is a commit dockhand made of the snapshot's files on top of `base`.
  For a baseline (`check --baseline`), `commit` is `base` itself, and the bundle holds it less its parent; the same fetch works.
- **`targets`** are in dependency order. Build them in this order.
  - `subport`, when present, is the subport to build from the Portfile.
  - `kind` is `substantive`, `revision-only`, or `unchanged`. `role` is `changed`, `also`, or `prerequisite`.
- **`tests`** is the test policy:
  - `declared`: run the declared tests, and a failure is advisory;
  - `required`: a test failure fails the target;
  - `skip`: don't run them.

## The result file

Write the result file at the `result` path:

```json
{
  "version": 1,
  "targets": [
    {"id": "libharbor", "outcome": "passed", "tests": "passed", "log": "libharbor.log"},
    {"id": "harbor-cli", "outcome": "failed", "phase": "install"}
  ]
}
```

- **`outcome`** is `passed`, `failed`, or `blocked`.
  - A `failed` target names the `phase` it stopped at: `lint`, `fetch`, `checksum`, `install`, or `test`.
  - A target left out of the file was not run.
- **`tests`** is `passed`, `failed`, `timed-out`, `none`, or `skipped`. `none` is the default.
- **`log`** is a log file for the target. A relative path is read from the request's directory. The default is `command.log`.
- **Blocked dependents.** When a target fails, every target that depends on it is recorded as blocked, whatever the script reports for it. An old build of a dependency never stands in for the one this branch changes.
