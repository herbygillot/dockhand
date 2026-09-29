# 2026-09-29: `submit --passing` takes no `--yes`

The ov run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#ov-through-adopt-edit-retry-and-submit---passing), finding 7) found that `submit --passing` needs a terminal even with `--yes`, leaving a script no batch submission. Checking it found more: `--passing` never read `--yes`, so it was silently ignored.

The person decided D11 on 2026-09-29: no batch `--passing`. Asking about each passing branch is deliberate, since publishing is a person's decision about an exact revision (principle 7). So `--passing --yes` is now refused before anything is looked at: "--passing asks about each branch, so it takes no --yes; dockhand submit --branch <name> --yes submits one without asking". The guide says so.

The submit test's `--passing` part covers it; its mutation, dropping the refusal, fails it.
