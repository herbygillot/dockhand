# 2026-09-30: a Git-fetched port assessed by its commits

Item 9's step 3 left a Git-fetched port's assessment holding as "not assessed yet", until batch 20's source record landed. With it merged, the rest of step 3 went in.

- **Compared through its forge.** A Git-fetched port is compared through its forge's archive of the commit each version's `git.branch` names (`engine.readCommits`).
  - **Commits:** each is resolved as a fresh clone would resolve it (batch 20's `resolveGit`, `git.CloneCheckout`). The base's is resolved now, not when the base was made, and the coverage says so, beside "read from the forge's archive of each commit, submodules left out".
  - **Archives:** they come through `upstream.Service.SourceArchive`. The repository is the port's forge PortGroup's, or else the one its `git.url` names on github.com or gitlab.com, which say by their addresses which repository a URL is.
  - **Endpoints:** `forge.ArchiveRepository` is new, on GitHub's documented archive link ("Download a repository archive (tar)"), fetched through `fetch` with its redirect rules, and on GitLab's documented archive endpoint ("Get file archive"), streamed. Both are bounded, at 512 MiB, as a distfile's download is.
  - **Readings:** a reading of a commit is kept by the commit, so an assessment made again asks the forge for nothing.
- **What holds, with the reason said:**
  - a `git.branch` that can't be resolved;
  - one that's an abbreviated commit, which only a clone expands;
  - a Git source that can't be read;
  - a repository on no forge dockhand reads.
- **A tag moved since the check** (batch 20's open item). `submit`'s plan resolves each Git-fetched target's ref again (`engine.movedSources`). One that names another commit than the check planned is a concern: "tool's git.branch v2 named bd356cb when check-7 planned it, and names 5fb88e4 now: the check built another source than this would submit". It holds a submission nobody reviews, and a person's preview shows it among the upstream lines. A ref that can't be read now isn't said to have moved, since the check's evidence says what it built.

**Proven:**
- `forge/github` and `forge/gitlab`: the archive fetched, not found, and bounded, with a commit required.
- `upstream`: the repository by PortGroup, by a GitHub or GitLab.com `git.url`, and refused for another host or scheme.
- The engine:
  - a local repository's tags compared by their commits' archives, with the forge asked nothing again for a later revision;
  - an unresolvable ref, and an abbreviated commit;
  - a moved tag held, one unmoved or unreadable not.
- Mutation testing: every mutant of `readCommits` is killed.
- Checks: the full suite, `vet`, `fmt-check`, `vendor-check`, `deadcode`, and `lint`.
