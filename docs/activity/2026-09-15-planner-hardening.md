# Planner hardening after the coverage review

## Context discovery

Reproduced successful but incomplete preparations for a multiline OS condition and a comparison assigned through `set modern [expr {...}]`: both changed the shared version but left the older-OS archive's checksum unchanged. Context discovery now scans command contents regardless of the enclosing command name or formatting. Each Darwin-major read must have an individually supported literal comparison; one recognized comparison cannot hide another unresolved read. Arithmetic/computed bounds remain an explicit uncertainty rather than a guessed boundary.

Added native preparation regressions for both missed-context cases and focused checks for unresolved aliases, mixed literal/variable bounds, arithmetic, and fractional bounds. Both affected archives are now downloaded and refreshed. The targeted profile and native preparation tests passed, including independent-pin preservation and existing architecture/OS coverage.

The scanner remains a conservative source analysis, not a general Tcl interpreter or proof of arbitrary PortGroup behavior. All code and fixtures in this pass are newly authored; no v1 comments or tests were copied.
