# MIT licensing and optional dependency helpers

Added the standard MIT license with the existing project's 2026 Herby Gillot copyright notice, and linked it from the introductory README. Removed the outstanding license prerequisite from the roadmap.

Clarified the planned Go/Rust dependency regeneration contract: go2port and cargo2port are optional host tools, required only when the selected update needs the corresponding helper. Missing executables must produce an actionable port/tool/block-specific error without retaining stale dependency checksums or blocking unrelated updates. Setup will report availability without requiring or automatically installing either utility. README helper guidance will arrive with working support.

Kept regeneration at priority 3, after the remaining exercise corrections and checksum preparation groundwork. Go and Rust will be implemented and validated independently through the existing preparation contract. No helper integration or setup behavior changed in this documentation pass.

Validation: verified the license matches the standard MIT text already used by v1, checked the README link and roadmap/design consistency, and ran the whitespace check. No executable code changed, so no code tests were needed.
