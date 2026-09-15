# Per-dependent Tart images

Added repeatable `--target-image port=image` for dependent verification. CLI parsing remains in `cli`; `app` binds image identities through Tart; the workflow carries immutable per-target configurations; `verify` applies them to the durable cohort plan. SQLite stores these choices in existing job options, requiring no schema migration. No new packages were needed.

Root targets use `--image`. Overrides are limited to the same platform and build policies, reject duplicates and incompatible flag combinations, and fail planning for names outside the discovered cohort. Each dependent retains its own and its preinstalled roots' Xcode requirements. Recovery uses recorded configurations and does not resolve image names again.

Validation covers CLI rejection before state access, intake binding, provider-default isolation, unmatched plan names, and the existing capacity-one/reopen/publication-gating lifecycle with a distinct downstream image digest and configuration. No new live VM build was run for this change; existing provider checks still validate image identity and guest capabilities at admission.
