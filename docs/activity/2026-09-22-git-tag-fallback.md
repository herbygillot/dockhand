# 2026-09-22: GitHub tags fall back to plain git

Release discovery reads a GitHub upstream's tags through the REST API.
When the API cannot answer, a rate limit, a refusal, a network failure,
the tags are now read with `git ls-remote` instead, which a public
repository answers anonymously. The API stays the first choice: it
alone can say a tag names a commit rather than a blob, and it is what
releases and manifest files still come from.

- `git.ListRemoteTags` lists a remote's tags by URL, with no checkout,
  outside any repository's configuration and without prompting for
  credentials. An annotated tag is peeled to the commit it names; a
  named lookup asks for the peeled line too, since an `ls-remote`
  pattern does not match it, and keeps only exact names, since a
  pattern also matches `release/v1.0` for `v1.0`.
- The GitHub adapter falls back on any API failure that says nothing
  about the tag. A 404 is the API's answer that the tag does not exist
  and is not second-guessed. When git fails too, both causes are kept,
  so a rate limit is still recognized and backed off. The fallback is
  reported at `-v`.
- `github.Config.CloneURL` says where repositories are read with git,
  `https://github.com` unless set; tests point it at local bare
  repositories, and the adapter's existing failure tests at an empty
  directory, so none of them reaches the network.

What git cannot do: tell a lightweight tag of a blob, such as git/git's
`junio-gpg-pub`, from a release tag. Such a tag never parses as a
version, so it is listed and ignored.

## In the cloud session that prompted it

The session's proxy allows GitHub API calls only for repositories
attached to it. `bump git-devel --dry-run` on Linux now lists git/git's
tags with git, selects `v2.56.0-rc2`, and confirms the tag, then stops
at the archive download from github.com, which that proxy refuses for
the same reason. The archive's checksums are GitHub's bytes, so git
cannot stand in there.
