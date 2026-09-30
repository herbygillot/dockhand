# 2026-09-30: variants in checks (item 8)

Design v3 promised `check --variants` for a single selected target, and `--variants each` evidence ticking the template's variants item. Neither existed, so `submit --tested-variants` ticked a box no check could evidence, and s2n-tls, whose tests run only under `+tests`, could never have them checked (the libuv, sqlit-tui, ouch, and s2n-tls run's finding 7).

The person decided three things (2026-09-30):
- `each` leaves out `universal`, and the implementer picks the rest;
- above a number of builds, `check` asks first;
- a passing `each` check ticks the box, and lists which variants it built.

## What was already there

Below the plan, variants were already carried through:
- `model.Target` has them;
- the Tart guest passes them to `port install` and to the check for tests;
- the command provider's request file carries them;
- reuse keys results by them.

The guest deactivates every active port before each target, and MacPorts keeps each variant set as its own installation, so a variant build after the port's default one needs nothing new. What was missing was everything above that.

## Commits

1. **`fe03fa1a`.** `macports.PortInfo.Variants` reads what a port declares, from Base's own record (`PortInfo(variants)` and `vinfo`): each variant's default flag, requires, conflicts, and description. Its test against real MacPorts found Base's own `universal` among them. `macports.ParseVariants` reads choices as MacPorts' command line takes them. A plan target's identity is its port's name and its variant choices, `s2n-tls +tests` (`model.Target.ID`), and the plan's `Validate` holds it to that.
2. **`719a433c`.** Planning:
   - The port reader evaluates with variants.
   - `planVariants` takes the one port `--only` names, or the branch's one changed port. It builds it with the variants in place of its defaults, or, for `each`, beside its default build: once with each variant it declares, but its defaults and `universal` (`VariantBuilds`). Each build is evaluated with its variants in each environment, and left out where the port doesn't declare them there.
   - `--only` keeps every build of the port it names, and a dependency on a port finds its build.
   - An exclusion is matched by the build's identity, rather than the port's name, which would have excluded its other builds too.
   - Refused: an undeclared variant, more than one port, `--variants` with `each`, and GitHub's workflow, which builds default variants alone.
3. **The command, and the evidence:**
   - `check --variants <+name -name | each>`.
   - A `Variants` line in the plan says what it builds. Every build is its own row in the plan, the results, and the pull request, where port names had been used.
   - Above 12 builds (targets times environments), `each` asks first; without a terminal, `--yes` builds them (`confirmVariantBuilds`).
   - `Evidence.VariantsBuilt` says whether an `each` check's builds of its port all passed. The PR's variants item is then ticked with "(dockhand built s2n-tls with each of +debug, +tests over its defaults)", and `submit` doesn't ask about variants.
   - `--accept` of a port with several builds takes the one that failed.

## Live, against the real tree

In a scratch clone, with s2n-tls given a revision bump, `check --plan --on tart:tahoe` printed:

```
--variants each
Changed     s2n-tls (revision only), s2n-tls +debug (revision only), s2n-tls +tests (revision only)
Variants    s2n-tls with its defaults, then with each of +debug, +tests over them (universal left out)
Order       s2n-tls → s2n-tls +debug → s2n-tls +tests

--variants +tests
Changed     s2n-tls +tests (revision only)
Variants    s2n-tls +tests, in place of its defaults

--variants +gui
--variants: s2n-tls declares no gui
```

`+debug` is the cmake PortGroup's. No VM was started, so the builds themselves are for the next dogfood run.

## Not done

- **A baseline of a variant build.** A baseline names its ports by name, so it would rebuild the port with its defaults at the base. Carrying the variants into the baseline comes next, if the dogfood run wants it.
- **A saved coverage profile**, for different variants on different ports, stays deferred, as the design has it.

## Tests

- `TestTheEvaluatorReportsTheVariantsAPortDeclares` runs against MacPorts.
- `TestVariantChoicesReadAsMacPortsTakesThem` and `TestATargetsIdentityHasItsVariants` cover reading choices and a target's identity.
- `TestVariantsBuildOnePortTheirWay` covers both forms, a variant missing on one release, and every refusal.
- `TestADependencyFindsThePortsBuild` covers a dependency finding the port's build, and `--only` keeping every build.
- `TestAVariantsCheckAnswersTheVariantsItem`, `TestTheVariantsFlag`, and `TestAVariantsCheckSaysWhatItBuildsAndAsksFirst` cover the tick, the flag, and the ask.
- `TestPlansRoundTrip` adds a plan with a variant build.
