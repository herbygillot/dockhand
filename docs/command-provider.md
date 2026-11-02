# The command provider

`check` can build on your own script: a build box, a VM you manage, or anything else. Dockhand gives the script a request file and reads back a result file. It cannot vouch for how the script built, so the results read "reported by <name>" (Design v3 §7).

## Setting it up

In `~/.dockhand/config.toml`:

```toml
[providers.command]
run = "~/bin/build-ports"   # run by sh, with the request file's path as $1
name = "my build box"       # labels its results; "command" when unset
capacity = 1                # how many checks run on it at once, serve's, a batch's, and yours together; 1 when unset

[build]
default = "command"         # the default for check, as it is where nothing else is set; --on command also selects it
```

`check` runs the command once per attempt. It retries environment failures up to a total of three attempts. It does not automatically retry a port build failure. Examples of such trouble: the command exits without writing a result file, or writes one that can't be read.

## How it runs

The command runs in a session of its own, with no terminal. A check that stops early interrupts it and everything it started, such as `port`, with SIGINT, and kills whatever still runs 30 seconds later. A check stops early when:
- it is canceled with `dockhand cancel`;
- `check --replace` replaces it;
- serve stops;
- you press Ctrl-C.

This prevents child processes such as `port` from keeping the MacPorts lock after the check stops. The script has no terminal, so it must run without prompts, including `sudo` password prompts. A command that requires a terminal fails immediately and records the error in `command.log`.

## The request file

The file is `request.json` in a directory of its own. Its path is also given as `$DOCKHAND_REQUEST`. The command runs in that directory, and its output goes to `command.log` there.

```json
{
  "version": 1,
  "run": "check-3",
  "attempt": 1,
  "execution": "command_7y62p4sigena6xlr",
  "bundle": "/…/logs/check-3/command-1/source.bundle",
  "ref": "refs/dockhand/check/check-3",
  "commit": "7e3f1a2…",
  "base": "4c1e2d0…",
  "platform": {"OS": "", "Version": "", "Architecture": ""},
  "tests": "declared",
  "targets": [
    {"id": "libharbor", "name": "libharbor", "portfile": "devel/libharbor/Portfile", "kind": "substantive", "role": "changed",
     "git": {"url": "https://github.com/harbor/libharbor.git", "branch": "v4.0", "commit": "1a2b3c4…"}},
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
- **`execution`** is this provider run's ID. The pull request's Tested on
  names it, and `dockhand logs command_7y62p4sigena6xlr` shows its
  evidence. Label your own logs with it to tie them together.
- **`targets`** are in dependency order. Build them in this order.
  - `subport`, when present, is the subport to build from the Portfile.
  - `variants`, when present, are the variants to build the target with, by name: `{"tests": true, "x11": false}` is `+tests -x11` on `port`'s command line. A target without it builds with its default variants. A target with variants has them in its `id` too, as `libharbor +tests -x11`, since a check may build one port more than once.
  - `kind` is `substantive`, `revision-only`, or `unchanged`. `role` is `changed`, `also`, or `prerequisite`.
  - `git`, present for a port MacPorts fetches with Git (`fetch.type git`), is what its fetch clones: `url` is its `git.url`, `branch` its `git.branch`, absent for the repository's default branch, and `commit` the commit `branch` named when the check was planned. A tag can be moved, so the fetch must check out that commit, and your script should say which one it did (`fetched`, below). `commit` is absent where dockhand couldn't read the repository's refs, or where `branch` is an abbreviated commit, which only a clone expands.
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
    {"id": "libharbor", "outcome": "passed", "tests": "passed", "log": "libharbor.log", "fetched": "1a2b3c4…"},
    {"id": "harbor-cli", "outcome": "failed", "phase": "install"}
  ]
}
```

- **`outcome`** is `passed`, `failed`, or `blocked`.
  - A `failed` target names the `phase` it stopped at: `lint`, `fetch`, `checksum`, `install`, or `test`.
  - A target left out of the file was not run.
- **`tests`** is `passed`, `failed`, `timed-out`, `none`, or `skipped`. `none` is the default.
- **`reference`**, optional, is your own name for the run, such as your CI's
  URL for it. Dockhand records it, and `dockhand logs` finds the run by it.
  A pull request names it instead of dockhand's ID when it's an `https://`
  link anyone can follow.
- **`log`** is a log file for the target. A relative path is read from the request's directory. The default is `command.log`.
- **`fetched`**, for a target with `git`, is the commit its fetch checked out. MacPorts clones into the port's `worksrcpath`, so after `port fetch` it is what `git -C <worksrcpath> rev-parse HEAD` says. A commit other than the request's `commit`, or other than one `branch` abbreviates, means the source moved since the check was planned: the target is recorded as failed at `fetch`, whatever `outcome` says, since it didn't build the source the check covers. A value that isn't a whole commit is trouble with the script. Without `fetched`, what the build fetched is unknown: the result stands for this check alone, and a later check of the same files doesn't take it in place of a build of its own.
- **Blocked dependents.** When a target fails, every target that depends on it is recorded as blocked, whatever the script reports for it. An old build of a dependency never stands in for the one this branch changes.

## An example: building on another Mac over SSH

This script builds a check on a Mac you reach over SSH, such as an older Mac whose macOS release no VM on this one can run. Save it as `~/bin/dockhand-over-ssh`, make it executable, and name the Mac in the command:

```toml
[providers.command]
run = "BUILD_HOST=oldmac.local ~/bin/dockhand-over-ssh"   # BUILD_HOST: the Mac, as ssh names it
name = "oldmac over SSH"
```

The other Mac needs Git, MacPorts in `/opt/local`, a clone of macports-ports (`~/macports-ports` there, or the path `BUILD_PORTS` names, absolute or from the login's home), SSH logins without a prompt, and a sudoers rule that lets the login run `port` without a password. This Mac needs `jq`, which macOS 15 and later include.

The script copies the bundle to the other Mac, fetches it into the clone there, and checks out `commit`. Then, for each target in order, it runs `port` on the target's Portfile with `subport=` and the target's variants: `lint`, `clean --work`, `fetch`, `checksum`, `destroot`, `install`, and, where the tests policy and the Portfile's `test.run` ask, `test`. A target whose dependency didn't pass is reported blocked, without building. A Git target's `fetched` is read from its `worksrcpath` after the fetch. Each target's `port` output is its log.

<!-- ssh-example: begin -->
```sh
#!/bin/sh
# dockhand-over-ssh: builds a dockhand check on another Mac over SSH.
#   BUILD_HOST   the Mac to build on, as ssh names it
#   BUILD_PORTS  a macports-ports clone there, absolute or from the login's
#                home; macports-ports when unset
set -eu

request=$1
host=${BUILD_HOST:?BUILD_HOST names the Mac to build on}
ports=${BUILD_PORTS:-macports-ports}
here=$(dirname "$request")

field() { jq -r "$1" "$request"; }
# quote makes a word safe on the other Mac's command line.
quote() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }
# remote runs a command line on the other Mac, where MacPorts is in /opt/local.
remote() { ssh -o BatchMode=yes "$host" "PATH=\$PATH:/opt/local/bin:/opt/local/sbin; $1"; }

bundle=$(field .bundle) ref=$(field .ref) commit=$(field .commit)
base=$(field .base) tests=$(field .tests) result=$(field .result)

# The bundle holds commit less base: the clone fetches base from origin
# where it hasn't it, then the bundle, and checks out commit.
portsdir=$(remote "cd $(quote "$ports") && pwd")
scratch=$(remote 'mktemp -d')
trap 'remote "rm -rf $(quote "$scratch")" || true' EXIT
scp -q -o BatchMode=yes "$bundle" "$host:$scratch/source.bundle"
remote "cd $(quote "$portsdir") &&
  { git cat-file -e $(quote "$base^{commit}") 2>/dev/null || git fetch -q origin; } &&
  git fetch -q $(quote "$scratch/source.bundle") $(quote "$ref") &&
  git checkout -q --detach $(quote "$commit")"

# run_port runs port(1) as root on the target's Portfile, into its log.
run_port() {
  remote "sudo -n \"\$(command -v port)\" -N -D $(quote "$portdir") $* subport=$(quote "$name") $variants" >>"$here/$log" 2>&1
}
# step runs a port action unless the target has already stopped; a failure
# stops it at the phase named.
step() {
  phase_of=$1
  shift
  if [ "$outcome" = passed ] && ! run_port "$@"; then outcome=failed phase=$phase_of; fi
}
# option is one of the target's Portfile options, as MacPorts opens it
# with the target's variants.
option() {
  remote port-tclsh <<TCL
package require macports
mportinit
set port [mportopen {file://$portdir} [list subport {$name}] {$variations}]
puts [[ditem_key \$port workername] eval {option $1}]
mportclose \$port
TCL
}

: >"$here/failed.txt"
: >"$here/results.jsonl"
count=$(field '.targets | length')
i=0
while [ "$i" -lt "$count" ]; do
  target=".targets[$i]"
  id=$(field "$target.id")
  name=$(field "$target | .subport // .name")
  portdir=$portsdir/$(dirname "$(field "$target.portfile")")
  log=target-$((i + 1)).log
  outcome=passed phase= tested=none fetched=
  : >"$here/$log"
  # The variants as port takes them, +tests -x11, each quoted for the
  # other Mac's shell, and as MacPorts' Tcl does, tests + x11 -.
  variants=$(field "$target.variants // {} | to_entries
    | map((if .value then \"+\" else \"-\" end) + .key | @sh) | join(\" \")")
  variations=$(field "$target.variants // {} | to_entries
    | map(.key + (if .value then \" +\" else \" -\" end)) | join(\" \")")
  # An ID can hold spaces, as a variant build's does, so jq compares them.
  blocked_by=$(jq -r --rawfile failed "$here/failed.txt" \
    "$target.depends_on // [] | map(select(. as \$d | \$failed | split(\"\\n\") | index([\$d])))
      | .[0] // empty" "$request")
  if [ -n "$blocked_by" ]; then
    outcome=blocked
    echo "$blocked_by did not pass" >>"$here/$log"
  fi

  step lint lint
  step fetch clean --work
  step fetch fetch
  if [ "$outcome" = passed ] && [ "$(field "$target | has(\"git\")")" = true ]; then
    worksrc=$(option worksrcpath) &&
      fetched=$(remote "git -c safe.directory='*' -C $(quote "$worksrc") rev-parse --verify 'HEAD^{commit}'") ||
      { fetched=; echo "which commit the fetch checked out wasn't read" >>"$here/$log"; }
  fi
  step checksum checksum
  step install destroot
  step install install
  if [ "$outcome" = passed ]; then
    if [ "$tests" = skip ]; then
      tested=skipped
    else
      declared=$(option test.run) || declared=no
      case $declared in
      yes | true | on | 1)
        if run_port test; then
          tested=passed
        else
          tested=failed
          if [ "$tests" = required ]; then outcome=failed phase=test; fi
        fi
        ;;
      esac
    fi
  fi

  [ "$outcome" = passed ] || echo "$id" >>"$here/failed.txt"
  jq -n --arg id "$id" --arg outcome "$outcome" --arg phase "$phase" \
    --arg tests "$tested" --arg log "$log" --arg fetched "$fetched" \
    '{id: $id, outcome: $outcome, tests: $tests, log: $log}
      + (if $phase == "" then {} else {phase: $phase} end)
      + (if $fetched == "" then {} else {fetched: $fetched} end)' >>"$here/results.jsonl"
  i=$((i + 1))
done
jq -s '{version: 1, targets: .}' "$here/results.jsonl" >"$result.tmp"
mv "$result.tmp" "$result"
```
<!-- ssh-example: end -->

A port the other Mac already has installed at the branch's version isn't reinstalled: `install` leaves it as it is, though `destroot` still builds it. The script exits without a result file when it can't reach the Mac or fetch the revision there, which `check` treats as environment trouble and retries.

What it can't do:
- **Dockhand evaluates the Portfiles on this Mac.** Which ports are targets, with which variants, and whether a target is tested, are decided when the check is planned, by this Mac's MacPorts. A Portfile that chooses by platform, such as `platform darwin 10 { … }` or a variant that's the default only on an old release, is read as this Mac's, not the other Mac's. The other Mac builds what its own MacPorts makes of each target's Portfile, but the check may name targets it would not, and miss ones it would.
- **The results don't name the other Mac's release.** The command provider declares no platform: `platform` in the request is empty, and its results are labeled with the provider's name, never a macOS release. A provider declaring its platform is unscoped work, not planned.

This page's tests run this script through the command provider, with `ssh`, `scp`, `sudo`, `port`, and `port-tclsh` standing in for the other Mac, so it keeps to the request and result files described here.
