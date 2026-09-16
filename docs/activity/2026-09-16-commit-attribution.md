# LLM commit attribution

Added the project rule that LLM agents may use `Assisted-By`, but must not identify themselves or other LLM agents with `Co-Authored-By`. Human authorship is preserved.

## Historical attribution

Added `Assisted-By: OpenAI Codex` to 153 historical commits whose creation outputs were found in this Codex session. The existing Claude help-grouping attribution was preserved. No attribution was added to four commits without direct creation records in that audit (original hashes):

- `3588c84b488`: docs: prioritize contribution and publication improvements
- `a385dca3ec5`: refactor(macports): share committed-source surveys and port selection
- `fa7a019ca4d`: feat(portedit): assess preparation with shared pre-download checks
- `bab8b1dcbeb`: feat(cli): add assess for port update readiness

Rebuilt commit objects with new messages and parent hashes, preserving all original trees, author/committer identities, author/committer timestamps and timezone offsets, and other non-parent headers. All 159 commits present before the rewrite were verified against their originals; messages outside the confirmed selection were byte-for-byte unchanged. No runtime code changed or runtime tests were needed.

The original history, commit map, and attribution audit are saved in `.git/dockhand/history-backups/assisted-by-20260916T054422Z/`. Only local main was updated; remote and legacy v0 refs were unchanged. The rewritten history has not been pushed.
