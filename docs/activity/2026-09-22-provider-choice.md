# 2026-09-22: provider choice out of app

Step 1 of the [resolution design](../resolution-design.md)'s sequence.
`app.buildResolver` was 111 lines of verification policy: the provider a
person named, or under auto Tart when a prepared image serves the
platform and GitHub's workflow when none does; GitHub refusing a local
test policy, a from-source build, or a variant choice; per-target images
bound for dependents; and a local failure preserved as an evidence
requirement for a preparation, so the branch is still made, or returned
for a verification. It read four service fields and was called from all
three bindings. It went first because the request fields that feed it
are the ones `app.Preparation` would otherwise have to keep when the
resolution replaces the rest.

`workflow/choice` holds it now, moved verbatim, behind two interfaces:
`Local`, a Tart build for a platform, with `LocalImages` for a named
image, and `Remote`, a GitHub workflow build. `Providers` names the
choice and `Resolver` returns the engine's `BuildResolver` for one
verification's options. It knows Tart's options and sentinel errors,
which is what makes it a leaf beside the engine rather than part of it.
`app` wires the Tart provider and a GitHub adapter that reaches the
person's fork through the client, the publisher's destination, and the
forge, which stays in `app` because that is composition. The three
bindings call `s.resolver`, which is the choice with the options.

The policy's table test moved with it, with a fake remote in place of
the HTTP server, and gained two cases the old test implied but did not
state: GitHub refusing local policies, preserved as a problem under auto,
and an explicit Tart failure preserved for a preparation and failed for
a verification. The fork check kept its server test in `app`. The
messages a person sees are unchanged.
