# The source as an observed value, and the archive asked of it

2026-10-25, Then's item 1 (C8-4 and C8-5 of the architecture review of
8c007979), first toward v0.6.0.

## The archive comes from the repository the commit was resolved in

The assessment resolved git.url and git.branch to a commit, then handed
the archiver the whole Portfile, and upstream's `gitRepository` preferred
the forge PortGroup's repository. A port keeping `github.setup owner
project` with git.url pointed at a fork asked owner/project for the
fork's commit: missing there, the update held as unassessed; present in
both, the wrong tree was compared, unsaid.

- upstream `SourceArchive` takes the resolved `model.GitSource`, its URL
  and commit. `gitRepository` reads the repository from that URL: on the
  forge PortGroup's instance where the URL's host is it, a self-hosted
  GitLab's among them, else on GitHub or GitLab.com by address. Release
  discovery keeps the PortGroup's repository, as before.
- engine's assessment passes the resolved source (`SourceArchiver`).

## A lookup that failed is looked at again

`resolveGit` turned a missing ref, a failed clone, and a timeout alike
into an `Unresolved` string, which the assessment said as `refusal.Failed`,
not transient, so `lookAgain` never made it again.

- model `GitSource.Missing` marks a repository read with no ref Ref names;
  `resolveGit` and `resolveTag` set it.
- The assessment says a missing ref as `UpstreamMissing`, looked at again
  a day later as a refused archive is, and an unreadable repository as
  `Network`, transient, made again at the next look.

## Tests

TestASourceArchiveIsTheForgesOfTheCommit gains the review's fork probe,
asking fork/project. TestAGitFetchedPortIsComparedByItsCommits checks a
missing ref's kind, and a repository that couldn't be read, then could,
compared with no tree change.
