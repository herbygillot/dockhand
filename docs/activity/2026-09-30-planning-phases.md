# 2026-09-30: planning's phases named, out of the engine

Item 6's fourth piece begins with the code-organization review's finding 4: `PlanCheck` was the longest function in the tree, and its evaluation closure mutated the plan, the candidates, and five parallel maps per environment. The roadmap kept planning in the engine until item 6 changed its input, so that it would move once.

## What moved

`internal/planning` decides what a check builds, where, and in what order, and reads nothing itself. The engine still reads what needs the checkout and MacPorts: each directory's ports in each environment, how each changed directory changed (`targetKind`), and an extra's directory. It then hands planning one `Input`.

- `Evaluated` is what one environment's evaluation says of one port: its eligibility, its dependencies, whether it needs Xcode, and whether it declares no tests. `planning.Evaluate` reads them from a `macports.PortInfo`, and an unreadable one is an error, which leaves the port unresolved as before. An `Evaluation` is one environment's, by target.
- The phases are functions, each of what came before it:
  - `Exclusions`: what each environment rules out, and why;
  - `Built`: what some environment doesn't rule out;
  - `Needs`: what each target needs first where it's built, and their union, which `--only` reads;
  - `Narrow`: what `--only` keeps;
  - `EnvironmentPlan`: each environment's own order, dependencies, Xcode needs, untested targets, exclusions, and unmet targets;
  - `Merge`: the plan's own order.
- `Decide` runs them. A dependency loop comes back as a typed `Cycle`, and the engine words it with the environment's description, as before.
- A baseline's per-environment limit is `planning.Limited`, which was the engine's private `rebuild`.

The engine's boundary test names `internal/planning`. Planning imports only `model` and `macports`.

## A plan's targets are their ports' names

`model.Plan.Validate` now refuses a target, planned or omitted, whose ID isn't its port's name (finding 4's clause). Every environment's plan and every result name a target by that ID. Planning has always made it so, so no recorded plan changes.

## Tests

- `TestAPlanIsDecidedPhaseByPhase` drives the phases directly, over two environments, with each reason for an exclusion, a limited extra, a dependency on itself, on a port not built, and on one built only elsewhere. It also covers opposite orders on the two releases, and the merge.
- `TestOnlyAndACycle` covers `--only` with a prerequisite, an extra named to `--only`, a mismatched input, and a cycle.
- `TestEvaluateReadsWhatPlanningNeeds` covers the port facts, with a `test.run` not read left unsaid.
- `TestPlanValidation` adds the two naming cases.
- The engine's planning tests pass unchanged.

Ten mutations of the phases each fail a test. Three survived the first pass, and the tests above were extended to kill them.

Four files earlier batches committed unformatted are `gofmt`'d, in a commit of their own.
