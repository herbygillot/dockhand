# Roadmap priorities after the side discussion

Updated `docs/roadmap.md` and this activity report; no implementation changes were made for this task.

Added the three explicit requests: freshly fetched authoritative MacPorts master as the base for new bumps (and previews), the exact Generated-by trailer on fully generated contribution commits, and Go/Rust dependency regeneration through go2port/go.vendors and cargo2port/cargo.crates, including cargo.crates_github where applicable.

Ordered work as upstream base selection, generated-commit attribution, outstanding exercise corrections, broader source/checksum preparation, Go/Rust dependency preparation, then dependent verification. Preserved the main thread's existing guest-agent and named-checksum/GitLab follow-ups. Expanded the named-checksum item to distinguish the single-distfile case from later multiple-distfile support. Moved the already listed authentication, progress, and migration guidance corrections into the ordered queue.

Considered Claude's four project reviews and the workflow-package review against the current commit history and activity reports. Did not reopen completed ledger, state, migration, workflow, Tart, PortIndex, or planner work. Added explicit lower-priority items for measured CLI/integration test organization, dependency-rule checks, unused exported API/scaffolding review, and documenting status as a read-only projection. Refined the existing comment/documentation cleanup items and made the design precedence for human corrections and unattended publication explicit. License selection remains a distribution prerequisite.

The accepted additions beyond the side discussion are the engineering items above and the expanded multiple-distfile preparation scope. Existing deferred decisions remain deferred. No implementation priorities from the reviews are treated as instructions without checking their continued relevance.

Validation: checked the roadmap diff for preservation of existing planned/design/deferred work and ran the whitespace check. No code tests were needed for these documentation changes. Committed the documentation at the user's request before handing the priorities back to the main session.

## Pull-request body follow-up

Added evidence-based PR bodies as priority 3, immediately after generated-commit attribution, and renumbered the remaining ordered items. Checked the local MacPorts PR template, v1's body renderer, v2's publication content builder, and recorded environment/step data. The current body comes from commit-message body text, explaining empty bodies for subject-only generated commits.

The item includes the requested submission link, environment summary, and conditional checklist. It explicitly distinguishes executed tests from the declared-test policy, source-only installs from normal binary dependency reuse, and known generated commits from human/amended commits. It retains manual checks without adding new automatic checking features and preserves human PR-body edits.

Additional implementation scope identified: persist exact guest macOS/CLT and provider version metadata where needed, and establish generated-message compliance before checking the full commit-guidelines claim. Current subject formatting alone and a Generated-by trailer are insufficient. The Trac guidelines page could not be retrieved because of its access protection, so no claim was made that every current formatting rule was verified. These are roadmap requirements only, not implementation changes.
