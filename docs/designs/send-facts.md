# `dockhand send-facts`: a Mac's toolchain, hardware and macOS facts, shown, then sent as a gist

A feature for v0.9.0, placed at the person's word ("Plan it for 0.9.0", 2026-11-05 02:46Z, in the architecture thread). Its issue, [#83](https://github.com/herbygillot/dockhand/issues/83), and the roadmap's entry say where it stands; this file is the design, architecture's of 2026-11-05, written at the person's ask (02:19Z: "a 'dockhand facts' command. Maybe have it ensure the user is logged in, capture facts, hardware and OS data, upload as a GitHub Gist if it's not too much data. … Again if there's no private or sensitive data in there."; 02:19:48Z: "Make it 'dockhand send-facts'"). Read on main 543ad194c: tools/facts (probe.tcl, tart.go, generate.go, README.md), internal/github/device.go, internal/command/setup (auth.go, work.go), internal/macos/facts.go, docs/usage/setup.md, README.md.

## What it is for

Two uses, one document. A person's Mac is a toolchain-facts source dockhand can't otherwise reach (Intel, another Xcode, an older macOS): the same facts the Tart probe gathers become a row of the facts table (architecture's modelled-context data capture note, 2026-11-05, its effort 6). And a bug report needs the same facts: the README asks for `dockhand --version`, the command and the log; the gist's link carries the rest.

## What it captures

- **Toolchain, as MacPorts Base sees it:** tools/facts' probe.tcl, embedded in the binary and run under this Mac's `port-tclsh` (found as dockhand finds programs: `--tool`, `*_BIN`, `[tools]`, PATH, then the prefix) as the invoking user. Its sections today: shell (sw_vers, uname, xcode-select, xcrun, xcodebuild, the CLT packages and SDKs, Xcode.app listings and SDKs, clang versions), host_checks (libxcselect, /usr/bin tools, /usr/include), base (Base's variables, developer_dir, xcodeversion, the prefix, the sysinfo header), base_tools and worker (compiler selection as a port worker sees it). Without MacPorts, the shell and host_checks sections alone, said in the output.
- **Hardware and macOS:** `sw_vers` product, version and build; `uname -r` and `-m`; sysctl `hw.model`, `machdep.cpu.brand_string`, `hw.ncpu`, `hw.memsize`, `hw.optional.arm64`, `sysctl.proc_translated` (Rosetta). Never `system_profiler`, whose hardware report carries the serial number and hardware UUID.
- **dockhand:** its version, commit and variant from its build info, and the MacPorts version from the base section.

## What it never sends

The user's name (probe.tcl's `whoami` fact is left out of this run), the hostname, environment variables, the home directory as a path, the config file, the registry's installed ports, the prefix's contents, serial numbers and hardware identifiers, network facts. A scrub runs over every value before the preview: the home directory becomes `~`; a value that still carries the user's name or the hostname (an Xcode kept under a renamed folder, say) is dropped and the preview names the key. The document is plain JSON of a few dozen facts, tens of kilobytes; a gist holds text files only and nothing here is binary, so there is no zip. A document over 512 KB is refused as a fault, since nothing it gathers can be that large.

## The command

```sh
dockhand send-facts            # capture, show, ask once, upload as a secret gist
dockhand send-facts --plan     # capture and show; send nothing
dockhand send-facts --out facts.json   # write the document here instead of uploading
dockhand send-facts --yes      # no question
```

1. Capture, then the preview: one line for the Mac ("MacBookPro18,2, Apple M1 Max, 10 cores, 64 GB; macOS 26.6 (25G83); Xcode 26.6, CLT 26.6; MacPorts 2.12.7; dockhand 0.8.0"), each section with its count, what the scrub dropped, the size, and "sent as a secret gist under <login>"; `-v` prints the whole document. On a terminal it asks once; `--yes` skips; without a terminal and without `--yes`, nothing is sent (the usual unattended rule).
2. The login is whatever dockhand's other commands use: `GH_TOKEN` or `GITHUB_TOKEN`, dockhand's Keychain login, or the GitHub CLI's (`Signin()`, as setup's Publishing line reads it). With none, on a terminal, send-facts offers the same device-flow login setup's offer runs, then goes on; without a terminal it refuses naming `dockhand setup github`. That is the "ensure logged in".
3. A gist needs the `gist` scope, and dockhand's login asks `public_repo` and `offline_access` alone (internal/github/device.go:98). The login's scopes gain `gist`, so new logins have it; a Keychain login made before is read for its scopes (GitHub's `X-OAuth-Scopes` header on the login check) and, lacking `gist`, send-facts says "your dockhand login was made before gist access; run dockhand setup github to log in again" before uploading anything. A `GH_TOKEN` is checked the same way; the GitHub CLI's login carries `gist` by default.
4. The gist is secret (reachable by link, unlisted), under the person's account, two files: `facts.json` and a `README.md` saying what it is and how it was made; its description is "dockhand send-facts: <model>, macOS <version>, Xcode <version>". The forge gains `CreateGist` on its GitHub side only; the gist goes to github.com whatever forge the ports checkout uses.
5. Output: the gist's URL and "paste it into your issue". `--json`: `{"gist": "<url>", "machine": {...}, "sections": {...}}`.

## The receiving side: tools/facts

`tools/facts sent <url or file>…` downloads gists into a directory, and `generate -sent <dir>` reads each as a row whose source kind is `sent` (From: the gist's URL, Date: its date, MacPorts: the base section's version), through the Tart reader, since the document's facts are the probe's. Precedence in `FactsTable.Lookup`: a Tart row (dockhand's own images) first, then a sent row, then a buildbot's, since a sent row carries the directory facts a buildbot's lacks. Several sent rows for one release, architecture and profile: the newest whose Xcode is the builder's (`BuilderXcode`), else the newest; this is the Xcode-key decision architecture's modelled-context data capture note names, and until it is made, sent rows fill only keys without a Tart row (every x86_64 row).

## Not taken

A zip (gists are text; the document is small). `--public` (a secret gist is shareable by link; one fewer flag). Posting into an issue instead of a gist (`public_repo` would do without the new scope, but a gist is the person's to delete, and it is what Herby asked for). Starting a login without a terminal. Hardware beyond the sysctl facts named.

## Usage docs, same commit

- docs/usage/setup.md, a short section "Send your Mac's facts" after "Sign in and choose somewhere to build": what the command gathers, what it never sends, the preview, `--plan`, `--out`, and that the gist's link belongs in a bug report and helps dockhand model Macs it can't run on.
- README.md's "Help and contributing" paragraph (line 272) gains, after "the relevant error or build log": "`dockhand send-facts` gathers your Mac's toolchain, hardware and macOS facts, shows them, and uploads them as a secret gist to link from the issue."
- `dockhand --help` lists send-facts beside setup; the command's help carries the "never sends" list.
- docs/development.md's facts-table paragraph names the `sent` source; tools/facts/README.md gains the `sent` command.
- agents.md's environment table is unchanged (no new variable).

## Tests

Command: fake programs on PATH (sw_vers, sysctl, a port-tclsh stub writing probe output), the capture without MacPorts saying so; the scrub (a home path to `~`, a value carrying the user's name dropped and named); the preview's words; `--plan` sends nothing; `--out` writes the file and sends nothing; no login refuses naming setup github without a terminal and offers the login with one; a login lacking `gist` refused with the re-login words; the gist request against the fake forge (secret, two files, the description); `--json`. tools/facts: a sent document becomes a `sent` row; the precedence (sent over buildbot where no Tart row). No guest test.

## Size and placement

About a third of a batch, inferred: the command and scrub, the forge's gist and the scope check, the login's scope, tools/facts' source, docs and tests. The Xcode-key lookup is its own small core item. Placed in v0.9.0 at the person's word (2026-11-05 02:46Z), small and self-contained beside the restructure, and it feeds #17's table work: [#83](https://github.com/herbygillot/dockhand/issues/83).
