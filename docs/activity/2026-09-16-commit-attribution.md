# LLM commit attribution

Added the project rule that LLM agents may use `Assisted-By`, but must not identify themselves or other LLM agents with `Co-Authored-By`. Human authorship is preserved.

## Historical attribution

Added `Assisted-By: OpenAI Codex` to all 157 historical Codex-assisted commits. Direct creation records established 153; the user subsequently confirmed the remaining four, which were completed in a side chat. The existing Claude help-grouping attribution was preserved. The four user-confirmed commits (original hashes) are:

- `3588c84b488`: docs: prioritize contribution and publication improvements
- `a385dca3ec5`: refactor(macports): share committed-source surveys and port selection
- `fa7a019ca4d`: feat(portedit): assess preparation with shared pre-download checks
- `bab8b1dcbeb`: feat(cli): add assess for port update readiness

Rebuilt commit objects with new messages and parent hashes, preserving all original trees, author/committer identities, author/committer timestamps and timezone offsets, and other non-parent headers. All 159 commits present before the rewrite were verified against their originals; messages outside the confirmed selection were byte-for-byte unchanged. No runtime code changed or runtime tests were needed.

The original history, commit map, and attribution audit are saved in `.git/dockhand/history-backups/assisted-by-20260916T054422Z/`. Only local main was updated; remote and legacy v0 refs were unchanged. The rewritten history has not been pushed.

## User-confirmed follow-up

Added the missing trailers to the four confirmed commits, verifying all 160 commits present before this second rewrite. Every tree, author/committer identity, both timestamps and timezone offsets, and every non-parent header was preserved. Only the four selected messages changed.

The pre-follow-up bundle, second hash map, composed original-to-current map, and verification report are saved in `.git/dockhand/history-backups/assisted-by-confirmed-20260916T061143Z/`. Remote refs remain unchanged; nothing has been pushed.
