# 2026-09-27: one plan per environment

Roadmap item 2. The [architecture and data-flow review](../reviews/2026-09-27-architecture-and-data-flow.md)'s finding 2: a plan was several partially synchronized views of an environment.

## Each environment's own plan

- **Before.** A plan had one list of targets for every environment, their union, in one dependency order. Around it:
  - each target's `DependsOn` was its dependencies in any environment;
  - `NeedsXcode` on the target listed environments;
  - `Dependencies` held each environment's by position in `Environments`;
  - exclusions named a platform, not an environment;
  - `Unmet` was a list on the plan.

  Two environments whose dependencies ran opposite ways made a cycle in the union, and the planner refused the plan, asking for a check of each on its own.
- **Now.** `model.Plan` keeps what is the branch's: its targets, with their kind and role, and the person's selection (`Omitted`, `Only`, `Also`). Each environment has its own `EnvironmentPlan` in `Plan.Builds`, found by the whole environment (`Plan.In`), holding:
  - `Order`: the plan's targets it builds, in its own dependency order, unmet ones included;
  - `Dependencies`: what each needs built first there, among those;
  - `NeedsXcode`, `Unmet`, and `Exclusions`, the last with their reasons and without a platform, since the environment is the plan's.

  `Validate` checks each environment's plan against the plan: one per environment, an order of the plan's targets with each after what it needs, and nothing both built and excluded. Every target is built somewhere.
- **The planner** keeps each environment's evaluation apart: what it defined, what MacPorts CI rules out there, what each port depends on, and whether it needs Xcode.
  - A port an environment didn't define is excluded there, "not defined there".
  - A dependency counts in an environment only when that environment builds it.
  - `--only` adds back the changed prerequisites any environment needs, since the selection is the branch's.
  - Each environment is then ordered by its own dependencies. A cycle is unresolved in the environment that has it, and named: "dependency cycle on command macOS 26 (Tahoe) arm64: harbor-cli → libharbor → harbor-cli".
  - The plan's own list merges the environments' orders: the first environment's, and what each other one adds, after what comes before it there.
- **The runner** sends each environment its own order, and each target with what it needs built first there. Jobs carry `engine.JobTarget`, a plan target with its dependencies in the job's environment, since a plan target no longer has any.
- **Exclusions are by environment,** not platform: `Excluded(plan, target, environment)` and `Plan.Excludes`. Two environments on one release can differ, as their tools can.
- **One rule for which results count** (`engine.Counts`). Evidence of the same files takes an earlier check's result for a target in an environment only when that check ran in the whole environment and planned the target there. Its selection otherwise, and its test policy, don't matter: from the same files a target builds the same, and D1 keeps each result's own policy.
- **Plans recorded before** read as per-environment plans (`sqlite.decodePlan`). Each environment's plan is what the old form said about it: the shared order less what it excluded, its positional dependencies, or each target's own where there were none, and its exclusions, needs, and unmet targets. No migration rewrites them.
- **Output:**
  - `check --plan` shows one `Order` where the environments agree, and each environment's where they don't.
  - A port every environment excludes for one reason shows once; one excluded in some names them.
  - The plan's JSON adds `builds`, each environment's `order` and `depends_on`, and keeps its other fields, now derived from the environments' plans. An exclusion's `platform` is now the whole environment, with its provider.

Tests:
- the model's validation of environment plans, with an order that differs between two environments;
- plans recorded in the old form, one environment and two, read back as environment plans;
- dependencies that run opposite ways on two platforms: each environment ordered on its own, and each provider sent its own order;
- a cycle in one environment, named;
- the rule for which results count;
- the plan preview's orders and exclusions, and the plan JSON's `builds`.

## A baseline rebuilds a port only where it failed

- **Before.** A baseline built each port in every environment of the check it explains, including where the port had passed.
- **Now.** `PlanBaseline` asks the planner to build each port only in some environments (`PlanRequest.where`), and the rest of that environment's plan excludes it with the reason (`rebuildWhere`):
  - where the check failed it at install or test, and elsewhere "check-4 didn't fail it there";
  - for a port named with `--only` that failed nowhere, everywhere the check built it, and elsewhere "check-4 didn't build it there".

  Master's own reasons come first: a port master doesn't define, or rules out, in an environment says so.
- **The report** (`writeBaselineResults`) sets a port beside the branch's result only where the baseline rebuilt it. Where the check failed it but master doesn't build it, it says why: "· not built at the base: replaced by other". Elsewhere there is nothing to compare, and it says nothing. The branch's result is found by environment, not by position.

Tests:
- a check failing a port on one of two releases, whose baseline rebuilds it on that one alone, and a named port that failed nowhere rebuilt on both;
- the report on two releases: a port rebuilt on one, and one master doesn't build where the check failed it.
