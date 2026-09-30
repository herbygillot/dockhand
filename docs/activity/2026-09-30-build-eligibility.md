# 2026-09-30: build eligibility, as MacPorts reads it

Item 6's second piece: the private-helper review's finding 1, with its follow-up's criteria, and the beekeeper-studio run's finding 3.

The planner's `ineligible` decided whether MacPorts CI would build a port with its own reading of the options. It matched `known_fail` against "yes", "1", and "true", missing Tcl's `on` and its prefixes. It split `supported_archs` on spaces, so the Tcl list `{arm64}` excluded an arm64 target. It had no way to say it couldn't read an option. And beekeeper-studio, which declares no `known_fail`, was excluded on macOS 12 as "known_fail": MacPorts defaults `known_fail` to yes where a port's `platforms` exclude the release, and the reason was its `platforms {darwin >= 23}`.

**The decision is `macports.BuildEligibility`.** It gives a port as eligible, excluded with the option behind it, or an error, which the planner records as an unresolved target, neither built nor excluded, as it does an unreadable `use_xcode`. The exclusions are `replaced_by`, `platforms`, `known_fail`, and `supported_archs`, each with what the option said, and a reason a plan shows: "its platforms, {darwin >= 23}, exclude this release".

**MacPorts answers the two booleans.** The evaluator now reports:
- whether the port is known to fail where it was evaluated, as MacPorts tests it, `string is true -strict`, in its own interpreter (`dockhand.known_fail`);
- whether the port's `platforms` admit the release, as Base's own `_handle_platforms` decides it (`dockhand.platforms_compatible`). That procedure makes its decision by setting a `known_fail` default, so the evaluator runs it again with Base's `default` briefly replaced to catch that, then puts `default` back. A Base without the procedure leaves the fact unsaid, and the exclusion reads "known_fail", as before. Calling a Base procedure rather than restating its rule keeps the decision MacPorts', at the cost of depending on a procedure Base doesn't document, which is noted here, as `PortInfo(portgroups)` was.

`supported_archs` is read as a Tcl list. A port evaluated without the facts, as a test's fake is, has `known_fail` read as Tcl reads a boolean.

Tests:
- `TestEligibilityReadsOptionsAsMacPortsDoes`: `known_fail on`, the list `{arm64}`, `noarch`, a `platforms` exclusion, and three unreadable options;
- `TestTheEvaluatorSaysWhyAPortIsKnownToFail`, live: `platforms {darwin >= 23}` excluded on darwin 21 and eligible on 25, and `known_fail on`;
- `TestAPlanKeepsAnUnreadEligibilityApart`: an unreadable one unresolved, and a `platforms` exclusion named in the plan.

Eight mutations each fail a test, one after the planner's test was added.
