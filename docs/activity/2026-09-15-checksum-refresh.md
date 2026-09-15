# Standalone checksum refresh

Implemented `refresh-checksums` using the existing preparation/verification/publication workflow. The editor shares archive hashing and syntax-aware replacement with version bumps, preserves version/revision and unrelated metadata, supports named or multiple direct archives, and rejects unsupported fetching before downloads. Identical source bytes finish without a branch, verification attempt, or publication. Cancellation retains the ordinary preparation scheduling rules.

New code: checksum-only editor orchestration and no-op workflow recognition. Extracted the shared archive refresh function from the existing version editor. No new package or database schema. Tests cover multiple archives, calculated-version and revision preservation, repeated no-op refresh, custom-fetch rejection, and combined publication/recovery through the existing action matrix.
