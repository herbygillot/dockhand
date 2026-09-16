# Planner hardening after the coverage review

## Context discovery

Reproduced successful but incomplete preparations for a multiline OS condition and a comparison assigned through `set modern [expr {...}]`: both changed the shared version but left the older-OS archive's checksum unchanged. Context discovery now scans command contents regardless of the enclosing command name or formatting. Each Darwin-major read must have an individually supported literal comparison; one recognized comparison cannot hide another unresolved read. Arithmetic/computed bounds remain an explicit uncertainty rather than a guessed boundary.

Added native preparation regressions for both missed-context cases and focused checks for unresolved aliases, mixed literal/variable bounds, arithmetic, and fractional bounds. Both affected archives are now downloaded and refreshed. The targeted profile and native preparation tests passed, including independent-pin preservation and existing architecture/OS coverage.

The scanner remains a conservative source analysis, not a general Tcl interpreter or proof of arbitrary PortGroup behavior. All code and fixtures in this pass are newly authored; no v1 comments or tests were copied.

## Host-dependent observations

The modeled evaluator now observes file opens (including process pipes), source-file loading, and directory enumeration as well as filesystem metadata queries and process execution. Relative paths are normalized in the worker during the intercepted access. Captured regular files remain permitted; external paths and opaque symlinks produce a coverage gap. Directory enumeration is conservatively reported rather than treated as a modeled platform fact. Host-access events do not record file contents.

Native evaluator regressions passed for external and captured reads/sources, relative filesystem queries, symlinks, process execution, declaration ownership, and session isolation. The native final evaluation remains uninstrumented. This expands explicit dependency detection; it does not turn a modeled context into a virtual machine or certify arbitrary environment/SDK behavior.
