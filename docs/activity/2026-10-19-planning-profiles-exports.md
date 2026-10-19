# Planning without macos, one profile vocabulary, the exports, and the plist

The roadmap's v0.5.0 part 1 item 4, "One owner for each rule": the
architecture review of 5897a82's R3-15, R3-16, R3-28, and R3-10's
signature. Also a note on f3e736c6: tart/provision's `setupPin` moved
with `tart.TooOld`, which changes no image, as its comment now says.

## R3-15: planning without macos

Planning imported macos to write the release a minmax profile left out,
and the plan read `Span` back by the reason's prefix.

- `planning.Excluded{Reason, Span}` is what `Exclusions` gives for each
  target, marked where `span` makes it; `EnvironmentPlan` copies `Span`
  into `model.Exclusion`, and `spanReason` is gone.
- `planning.Input.Releases` are each environment's release as a person
  names it, which the engine fills with `macos.ReleaseName`; planning reads
  nothing of macOS, as docs/architecture.md says of it.
- Test: the `Span` marks in `TestSpanBuildsEachPortOnItsOldestAndNewest`.

## R3-16: one profile vocabulary

config exported `ReleaseRules` and `BuiltinProfiles`, which nothing
outside it read, and the engine had `RuleHost`, `RuleMinMax`,
`RuleMacPortsCI`, and `RuleAll` of its own.

- config owns them: `config.ReleaseRule`, a type, with `RuleHost` and the
  rest of that type, and `ProfileLocal`, with its lists unexported. The
  engine's `Profile.Rule` is a `config.ReleaseRule`, and `Profile.List`
  gives one. The engine imports config for these names only, never its
  `File` or `Load`, as engine/boundary_test.go's `engineImports` says, so
  X1's composition still holds. Config checks a file against them, and the
  engine applies them. The architecture thread agreed to the new import on
  that condition, the type.
- R3-21's second half, one launchd plist producer: the host's serve agent
  and the provider's guest plist already go through `macos.LaunchdJob`.
  Only tart/provision's `agentPlist`, inside every image, is hand-written;
  moving it changes what images hold, which raises `tart.SetupProtocol` and
  has everyone make their images again. With the architecture thread, it
  waits for SetupProtocol's next raise, for the roadmap's when-touched list.
- Test: `TestEveryRuleConfigTakesIsADockhandProfile`.

## R3-28 and R3-10's signature: the exports

Unexported, since nothing outside their package reads them:
`engine.ShipTarget`, `ShipOptions`, `Engine.Ship`,
`OutdatedPort.UncheckedReason`, and `TagMovedWords`;
`github.SpendLogVariable`; `depblock.ResolvesOnline`; `macos.AgentWait`
and `ErrStillLoaded`; `distfetch.ErrHTMLPage`. `distfetch.MirrorsBehind`
stays exported, since fetch reads it.

`Engine.Held(ctx, plan)`, with a context it ignored and an error always
nil, is `SubmitPlan.Held()`; ship's `submitPassed` reads it. The rest of
R3-10, `ApplySubmit` holding itself, is part 4.1's.
