# Publication follow-ups and authentication roadmap

## Scope

Recorded the approved addition of `dockhand auth login` to the implementation queue and prioritized the publication gaps observed in the chezmoi workflow. The working tree was clean at the start; executable changes were already committed as `8039d77`.

## Decisions recorded

- Native login is planned as browser-based GitHub device authorization with credentials in macOS Keychain. It must work without `gh`, a ports checkout, or a workflow database. Application registration is a prerequisite.
- First implement credential discovery and authenticated publication preflight. The current environment-only API authentication allowed public planning reads and a separately authenticated Git push, then failed when creating the PR. An authenticated preflight must not be described as a guarantee of later repository permissions.
- Next unify the relevant verification-selection policy for combined and standalone publication. The prepared chezmoi tree already had passing evidence, but combined publication stopped on missing image configuration before considering reuse.
- Keep branch-based wait/cancel and verification setup/settings in the queue. Native login follows the minimal publication authentication work and remains explicitly unimplemented.

Only `docs/cli-design.md`, `docs/components.md`, and this activity report changed. No code, credentials, application registration, database state, or remote repositories were modified. No v1 content was copied.

## Validation

Reviewed the roadmap against the current credential loading, combined verification planning, standalone publication binding, and fixed job-scope attachment contracts. `git diff --check` passed; executable tests were not rerun for documentation-only changes.
