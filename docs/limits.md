# Dockhand's limits

Every size, time, concurrency, and retry bound in dockhand's production code, and the guards that refuse input. Most are fixed in code; the few you can change are marked with their config key, and those `docs/usage.md` mentions are marked "in usage.md". Paths are under `internal/`. A limit that changes changes here too.

The sweep that wrote this (2026-10-01) found gaps among them: those batch 25 fixed are said where they stand now, and the rest are [batch 26](roadmap.md), each marked "gap" below.

## What you can change

These config keys are the only knobs on any limit. No flag or `DOCKHAND_*` variable tunes a time or size bound; `DOCKHAND_INDEX_MIRROR`, `DOCKHAND_INDEX_CACHE`, and `DOCKHAND_READING_CACHE` change only where things are fetched from or kept.

| Key | Default |
| --- | --- |
| `providers.tart.test_timeout` | 30 min per target's tests |
| `providers.{tart,github,command}.capacity` | 1 / 2 / 1 checks at once |
| `serve.submit_limit` | 10 pull requests a day |
| `serve.outdated_at` | 07:00, the daily look |
| `cleanup.after` | 7 days for caches, archives, events |
| `cleanup.min_free` | 30 GB free before cleanup runs |
| `cleanup.automatic` | whether cleanup runs on its own |

## Downloads and the network

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| Distfile download, size | none | any size downloads, at the person's word (2026-10-01) | `portedit/archives/download.go` |  |
| Distfile download, time | 3 min without data | "no data arrived for 3m0s, so dockhand gave up on it"; a download that keeps arriving goes on, and its wait for a response is bounded the same | `portedit/archives/download.go`, `fetch/stall.go` |  |
| Forge archive of a commit (Git-fetched ports) | none | any size downloads | `upstream/source_archive.go` |  |
| Livecheck page | 16 MiB | too large; the lookup's result is unknown | `upstream/http.go:130` |  |
| Livecheck document through GitHub's API | 16 MiB | "document exceeds the 16 MiB listing limit" | `forge/github/documents.go:47` |  |
| Manifest read from a forge (go.mod) | 1 MiB | "exceeds N bytes" | `upstream/manifest.go:14` |  |
| create's project files at the release tag | 4 MiB (≈1 MB in practice) | GitHub's contents API fails first past ≈1 MB | `engine/create.go:424` | gap |
| PyPI JSON | 8 MiB, through `fetch.Open` | "its JSON is larger than the 8 MiB dockhand reads of a release" | `pypi/pypi.go` (`maxRelease`) |  |
| Mirror's PortIndex | 128 MiB, no timeout | "Mirror index unavailable, indexing in full" | `macports/portindex/mirror.go:83` |  |
| Tart registry token | 1 MiB | decode error | `tart/registry.go:143` |  |
| Redirects | 10 | "fetch: unsupported redirect" | `fetch/fetch.go:103` |  |
| Error body read for a reason | 512 B read, 200 chars kept | the rest is dropped | `fetch/fetch.go:47,55` |  |
| FTP dial | 30 s | "fetch: <dial error>" | `fetch/ftp.go:31` |  |
| HTTPS probes of plain-HTTP URLs | 4 at once, 10 s each | the URL is said not to answer over HTTPS | `engine/https.go:24,84` | in usage.md |
| GitHub API pace | 80 ms apart (750/min) | requests wait their turn | `github/pace.go:21` | in usage.md |
| GitHub rate limit | a read waits up to 2 min for the reset, or Retry-After, else 1 min, then asks once more | past that, or for a write, never retried: "GitHub's rate limit for your login resets at 14:05, in 23 minutes"; nothing is asked while limited | `github/ratelimit.go` (`rateWait`) |  |
| API page size | 100, no page cap | every page is followed | `forge/github/repository.go:17` |  |
| Pull request search | one page of 50 | more aren't seen | `forge/github/pullrequests.go:148` |  |
| git ref resolved while planning a check | 1 min per git.url | the target is unresolved, and the check goes on | `engine/plan.go:204` |  |
| Any other HTTP request | 30 s to connect, 10 s for TLS, 1 min for a response | the request fails; a body that stalls after its headers still waits (batch 26) | `fetch/client.go` |  |
| GitHub sign-in (device flow) | GitHub's interval, 5 s | until the code expires | `github/device.go:51` |  |

## Archives and the source comparison

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| Archive walk (tar, any compression) | 4 GiB uncompressed, all read through | "exceeds scan limit: it holds more than the 4 GiB dockhand reads of one, uncompressed"; holds | `archive/archive.go` |  |
| Zip archives | 4 GiB of what's read | as a tar archive's; a member not read costs nothing | `archive/archive.go` (zip branch) |  |
| diff --archive extraction to disk | 4 GiB per archive, the walk's bound | writes both archives whole, up to it | `engine/archivediff.go:143` |  |
| A file the comparison reads (license, build file, manifest, Cargo.lock, package.json) | 1 MiB each | kept truncated; "larger than the 1024 KiB the comparison reads"; holds, but for go.mod, Cargo.toml, Cargo.lock, package.json | `project/read.go:71` | in usage.md |
| License text moved between files | ≤ 10 other lines | past that, the changes stand as changes | `sourcecompare/compare.go:191` |  |
| Symlink target in a snapshot | 4096 B | "symlink target too long" | `git/snapshot.go:184` |  |

## Patches and dependency manifests

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| Source file a patch touches | 64 MiB | "exceeds the size limit"; every patch unchecked | `macports/patchcheck/patchcheck.go:296` |  |
| Compressed patch (.gz, .bz2) | 64 MiB decompressed | refused, "decompresses to more than the 64 MiB dockhand checks of a patch"; that patch unchecked | `macports/patchcheck/patchcheck.go` (`decompress`) |  |
| patch's output | 1 MiB kept | the rest is dropped; patch's exit status decides | `macports/patchcheck/patchcheck.go` (`subprocess.Spec.Drain`) |  |
| patch --version output | 64 KiB | error | `macports/patchcheck/patchcheck.go:327` |  |
| go.mod or Cargo.lock in an archive | 16 MiB | "dependency: oversized NAME" | `macports/dependency/archive.go:14` |  |
| go2port or cargo2port | 10 min, 16 MiB of output | deadline or output error | `macports/dependency/generate.go:28` |  |

## Port index

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| One index entry | 128 MiB | "invalid entry length" | `macports/portindex/reader.go:235` |  |
| A PortIndex.quick line | 64 KiB | bufio.ErrTooLong | `macports/portindex/reader.go:150` |  |
| Generations tried as seeds | latest + 8 recent | else the mirror, else a full pass | `macports/portindex/cache.go:30` |  |
| Mirror bracket margin | 2 h | below the mirror's Last-Modified | `macports/portindex/mirror.go:41` |  |
| portindex run | no timeout | "can take minutes" | `macports/portindex/index.go:451` | gap |
| MacPorts runtime probe | 30 s | "probing the MacPorts runtime…" | `macports/portindex/index.go:31` |  |
| Waiting on another process's indexing | no bound | "Waiting for another process…" | `macports/portindex/cache.go:165` | gap |

## Evaluating Portfiles and running tools

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| Tcl interpreter handshake | 30 s | "rpc: no handshake within 30s" | `tcl/rpc/session.go:54` |  |
| Interpreter output line with no newline | 1 MiB | the session breaks | `tcl/rpc/session.go:23` |  |
| Interpreter reply frame | 16 MiB | the session breaks | `tcl/rpc/session.go:25` |  |
| Interpreter stdout not yet read | 32 MiB | "child output queue overflowed" | `tcl/shell/proc.go:16` |  |
| Interpreter stderr kept | last 64 KiB | older bytes dropped | `tcl/shell/proc.go:14` |  |
| A Portfile evaluation call | no timeout | only the command's end stops it | `macports/eval` | gap |
| Interpreter close | 2 s, then kill | — | `tcl/shell/proc.go:55` |  |
| A process after cancel | 2 s (git 1 s) | pipes closed | `subprocess/subprocess.go:35` |  |
| outdated lookups at once | min(8, max(2, CPUs)) | the rest queue | `outdated/outdated.go:140` | in usage.md |
| Interpreters per multi-platform observation | min(8, max(2, CPUs)) | the rest queue | `portedit/observe/observation.go:96` |  |
| Platforms a port is observed on | Darwin 8–30, ≤ 40 profiles | inconclusive | `portedit/observe/platform_observations.go:42,104` |  |
| Fetch procedure depth followed | 5 | refused | `macports/fetchguard/effect.go:59` |  |

## Checks and the runner

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| Attempts per environment | 3, retried at once | "failed 3 times for reasons of its own" | `model/execution.go:14` |  |
| Earlier results tried for reuse | 5 newest | per target and environment | `engine/reuse.go:18` |  |
| --variants each builds before asking | 12 | asks, or needs --yes | `command/check.go:923` | in usage.md |
| A build log's line read for a failure's likely cause | 1 MiB | passed over, still counted, and the log read on | `buildlog/buildlog.go` (`maxLine`) |  |
| Cancel requests checked | every 1 s | — | `engine/runner.go:359` |  |
| check follows the run | every 200 ms; waitFor every 500 ms | — | `command/check.go:491,550` |  |
| watch | 1 s poll, 30 s redraw | — | `command/watch.go:21,25` |  |

## Tart builds

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| Releases built at once per check | 2 | the rest wait | `buildenv/tart/provider.go:630` | in usage.md |
| VMs running on the Mac | 2 | waits, polling every 30 s, with no bound | `buildenv/tart/provider.go:641` | in usage.md |
| Tests per target | 30 min, then TERM, 10 s, KILL | timed-out; fails only under --tests required | `buildenv/tart/provider.go:70 · guest.tcl` | in usage.md · `providers.tart.test_timeout` |
| Guest lint, fetch, and install | no deadline | only the VM's end stops them | `buildenv/tart/guest.tcl` | gap |
| Clone to appear in Tart's listing | ≈ 30 s | goes on | `buildenv/tart/provider.go:592` |  |
| Guest IP | 5 min | infrastructure failure, retried | `buildenv/tart/machine.go:132` |  |
| SSH to accept | 4 min, every 3 s | "the guest never accepted SSH" | `buildenv/tart/provider.go:525` |  |
| Results polled | every 10 s | — | `buildenv/tart/provider.go:552` |  |
| Guest unreadable | 1 min | infrastructure failure, retried | `buildenv/tart/provider.go:818` |  |
| SSH connection | 10 s connect; dead after ≈ 60 s silent | — | `tart/channel/channel.go:121` |  |
| File transfers | 3 tries on a digest mismatch | "arrived as N bytes" | `tart/channel/transfer.go:20` |  |
| Command output over SSH | 1 MiB | output-limit error | `tart/channel/transfer.go:125` |  |
| Reading a guest file | no cap | held in memory | `tart/channel/transfer.go:96` | gap |
| Stopping a VM | 1 min per stage; leftovers 30 s | — | `buildenv/tart/provider.go:519 · machine.go:124` |  |

## Tart setup

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| SSH during setup | 4 min, every 2 s | "did not accept SSH within 4m0s" | `tart/provision/connect.go:19` |  |
| Guest agent ready | 5 min | "did not become ready" | `tart/provision/guest.go:38` |  |
| launchd registration | 120 × 1 s | "Timed out registering" | `tart/provision/agent.go:69` |  |
| softwareupdate --list | 6 tries, 15 s apart | — | `macos/install.go:55` |  |
| Free space for Xcode | 60 GB | "need at least 60 GB" | `macos/install.go:13` |  |
| Guest disk | 100 GB raw, 125 GB ASIF | — | `tart/provision/vm.go:93` |  |
| Guest CPUs and memory | CPUs ÷ 4; max(8 GB, 2 GB × CPUs) | — | `tart/provision/vm.go:118` |  |
| A listing that raced a delete | 5 tries, 500 ms apart | ErrListingRaced | `tart/images.go:68` |  |
| An ASIF VM blocking the listing | every 15 s, no bound | — | `tart/provision/native.go:72` | gap |
| Cleanup after a failed setup | 2 min | "delete it with tart delete…" | `tart/provision/provision.go:616` |  |

## GitHub Actions and the command provider

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| A run to appear | 10 min, polled every 30 s | "GitHub started no run… are Actions enabled?" | `buildenv/ghactions/provider.go:136` |  |
| A run to finish | no bound | — | `buildenv/ghactions/provider.go:213` | gap |
| Cancelling a run on GitHub | 30 s | — | `buildenv/ghactions/provider.go:200` |  |
| Job log | 64 MiB | kept to it, and the kept log ends saying it was cut | `buildenv/ghactions/github.go` (`maxJobLogBytes`) |  |
| A job log line | 4 MiB | passed over, and the log read on; the workflow's markers are short | `buildenv/ghactions/logs.go` (`maxLogLine`) |  |
| The command provider's script | no timeout; on cancel SIGINT, 30 s, SIGKILL | — | `buildenv/script/script.go:166` |  |

## serve and cleanup

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| serve's loop | every 2 s | — | `command/serve.go:26` |  |
| Reading pull requests | every 5 min | — | `command/serve.go:29` | in usage.md |
| Pull requests opened unattended | 10 a day | "waits for tomorrow" | `engine/serve.go:647` | in usage.md · `serve.submit_limit` |
| The daily look | 07:00 | a failure waits a day | `engine/serve.go:492` | in usage.md · `serve.outdated_at` |
| Checks at once per provider | tart 1, github 2, command 1 | runs wait | `config/config.go:243` | in usage.md · `providers.*.capacity` |
| Cleanup | once a day | a failure waits a day | `engine/clean.go:744` | in usage.md |
| Caches, kept archives, events, sessions | 7 days | pruned | `config/config.go:155` | in usage.md · `cleanup.after` |
| Free space that triggers cleanup | 30 GB; a 1 h pause when low | — | `config/config.go:123 · engine/clean.go:791` | in usage.md · `cleanup.min_free` |
| Tart's vanilla images | 30 days unused | removed | `engine/clean.go:739` | in usage.md |
| Database copies kept at a migration | 30 days | removed | `store/sqlite/sqlite.go:90` | in usage.md |
| A migration's copy left unfinished | 1 h | removed at the next migration; never counts as a kept copy | `store/sqlite/sqlite.go` (`abandonedCopies`) | |
| Check logs (~/.dockhand/logs) | never removed | 780 MB across 62 checks today (D6) | `engine/runner.go:548` | gap |
| Reading cache | never removed | only a reader version bump clears it | `project/cache.go` |  |
| Abandoned scratch run roots | 10 min | swept | `scratch/scratch.go:30` |  |
| Notifications | 10 s | — | `command/serve.go:146` |  |

## Store and coordination

| Limit | Value | When it's hit | Where | |
| --- | --- | --- | --- | --- |
| Heartbeat | every 5 s | a failed write is said once and tried again at the next beat | `coord/coord.go:78,161` |  |
| A session judged hung | 2 min without a beat | its leases can be taken | `coord/coord.go:85` |  |
| Leases | no expiry | held until released, ended, or judged dead | `coord/coord.go:247` |  |
| SQLite busy wait | 5 s | — | `store/sqlite/sqlite.go:101` |  |
| One transaction | 30 s | context.DeadlineExceeded | `store/sqlite/sqlite.go:104` |  |
| Connections | 4 | — | `store/sqlite/sqlite.go:132` |  |
| Write-ahead log kept | 64 MiB | trimmed at checkpoint | `store/sqlite/sqlite.go:60` |  |
| File locks (branch, image, index) | polled every 25 ms, no bound | another waits | `filelock/filelock.go:53` | in usage.md |
| Event pages | 500 (store default 1000) | — | `engine/runner.go:1127` |  |

## Guards that refuse input

These don't bound how much; they refuse what isn't safe to read or run.

- Archive members: absolute, unnormalized, or escaping paths are refused; links and devices aren't extracted.
- A manifest member that isn't a regular, clean path, or appears twice, is refused.
- Patches: names leaving the source tree are skipped; patch.dir outside it, or patch.pre_args beyond -pN -t -N -l -E -f -u -c --binary, leaves patches unchecked; patches run only as a dry run.
- cargo.dir must be under the source directory.
- Fetching: http and https only, with a host and no credentials in the URL; at most 10 redirects, never from HTTPS to HTTP.
- FTP: anonymous only.
- GitHub's API: a redirected write is refused, as is a redirect off the API's origin.
- A job log URL must be https, and is fetched without your GitHub credentials.
- Forge file reads need a full commit ID and a relative path with no "..".
- Livecheck: http or https, no credentials, and livecheck.ignore_sslcert false.
- Cargo Git crates: only https://github.com repositories.
- Master sites: http, https, or ftp, no credentials; each distfile literal and unique.
- archives.CheckPolicy refuses fetch credentials, non-standard fetch types, an ignored SSL certificate, and vendored sources it can't set aside.
- Local patches must resolve inside the port's directory, and be regular files, not symlinks.
- A download that's HTML, empty, or content-encoded is refused; a shipped archive must match its sha256, rmd160, and size (md5 or sha1 alone, which MacPorts has retired, isn't compared).
- Snapshots: no .git paths, backslashes, or NUL; only normal file modes; symlinks may not point outside.
- A Portfile that leaves the snapshot, is a symlink, or isn't a regular file is refused.
- Staging: payload names are checked; the commit and tree must agree; every target must be indexed where expected.
- Tart results: the protocol must match, targets must be ones asked for, logs bare names, fetched commits valid, kept archives under MacPorts' software directory.
- Kept binary archives must match their sha256 before signing.
- Tart never overwrites an existing VM.
- The portindex tool must be a regular executable whose digest is pinned.
- Tcl messages that aren't valid UTF-8 are refused both ways.

## Rules, not limits

MacPorts' commit-message style, which `submit` warns on: a subject over 60 characters (`macports/commitrules/rules.go:83`), and body lines over 72 (`rules.go:86`). Display cuts are cosmetic: quotes at 120 runes, patch detail at 300 characters, a fetch error's reason at 200, a Tart runner log's last 400.
