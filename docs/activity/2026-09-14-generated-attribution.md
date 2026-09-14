# Generated contribution attribution

Authored contribution-message rendering in prepare: generated commits retain the MacPorts subject and optional reason, followed by exactly one `Generated-by: [dockhand](https://github.com/herbygillot/dockhand)` trailer. Generation is deterministic; the low-level Git writer and human checkout, verification, adoption, and publication paths do not add attribution.

Recorded the original generated commit on the contribution. This immutable provenance survives branch renames and amendments, allowing publication to distinguish an untouched generated commit from a retained or copied trailer. SQLite schema 12 adds the optional identity; existing contributions remain unknown rather than guessed. No attempt is made to infer provenance from old commit text.

Validation covers blank/reason/already-attributed message bodies, workflow-generated attribution and provenance, stored provenance round-trips and immutability, and schema migration tests.

The prepare, SQLite, and workflow test suites passed, including the existing human-adoption/publication tests.
