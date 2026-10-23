# Dockhand architecture review evidence

These four regression probes reproduce findings 1–4 of the companion [review](../2026-10-22-architecture-8c007979.md) at commit `8c007979`. They run through Go overlays. Probe sources use a `.go.txt` suffix so ordinary package discovery does not include them. They deliberately assert the expected correct behavior, so they fail on the reviewed commit.

Run from this directory with `sh run-probes.sh /path/to/dockhand2`. The default checkout is `~/Source/dockhand2`. The tests use existing repository test fixtures; the watch probe needs permission to bind local mock servers. No overlay source is written into the checkout.

- `watch_context_test.go.txt`: the displayed check's logs must execute in the same invocation context.
- `engine_context_test.go.txt`: link declarations must stay scoped to their environment; standing acceptances must reach candidate selection.
- `source_identity_test.go.txt`: the assessment archive must come from the repository named by the actual Git source.

`existing-suite.log` records the unmodified suite run. `regression-probes.log` records the four expected failures. The overlay runner exits nonzero while any probe fails.
