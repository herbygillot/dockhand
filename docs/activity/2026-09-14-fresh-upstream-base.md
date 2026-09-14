# Fresh upstream base for new contributions

New bumps and revision bumps, including previews, now fetch authoritative MacPorts master directly, independently of fork/upstream remote names. Intake freezes its commit, tree, and base before evaluation and acceptance; retries use those recorded objects. Failed fetches do not fall back to local source. Removed the new-bump `--branch` option; existing contributions continue through verify/publish. A possible future `--from-branch` escape hatch is deferred pending a concrete need.

Authored a Git fetch primitive using unique temporary refs, with bounded cleanup even after cancellation. It disables FETCH_HEAD, remote-tracking updates, tags, recursive submodule fetching, and automatic maintenance. Concurrent fetches do not share a result ref. No checkout, local branch, index, or additional lock is involved. Integration no longer requires a moving local base branch to match a previously accepted commit. Workflow receives and checks immutable source rather than selecting a branch again.

Validation covers concurrent fetches, changed/deleted upstream branches, cached stale local refs, preserved FETCH_HEAD and refs, remote-name independence, preview/accepted-path fetch failure, and successful integration after local branch movement. Updated contributor and design documentation for the new behavior.

The full Go test suite and whitespace check passed. No live VM or publication was needed for this source-selection change.
