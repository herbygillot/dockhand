# Branch, Actions, and publication progress

GitHub admission reports the fork branch, commit, run identity, attempt and URL. Observations record workflow status and update single-target job progress, replacing stale preparation text. Expected run discovery is displayed as waiting progress while actual provider errors retain their retry diagnostics. Status shows queued/running workflow state and its fork branch. Publication planning, push, request and confirmation messages name the verified branch and commit used for the PR.

Tests follow durable driver progress through run discovery, queued and running observations, status rendering, and combined verification/publication retaining the prepared branch and commit. The additional workflow status and saved run URL use existing JSON storage; no schema migration is needed.

Validation: GitHub provider, CLI, and workflow package suites passed.
