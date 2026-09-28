// Package github supplies the GitHub client and authentication shared by forge
// and verification adapters.
//
// It selects credentials, constructs go-github clients lazily, implements device
// authorization and identity checks, and applies redirect and rate-limit handling.
// The adapters own repository, pull-request, and Actions operations; callers own
// credential persistence and user interaction.
//
// It also reads and writes GitHub's addresses: the repository a Git remote or a
// web page names, a repository's remote, and a pull request's pages.
package github
