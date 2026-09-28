# 2026-09-28: a Go module the build already had is no addition

The hugo exercise's chezmoi run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#chezmoi-with-bump), finding 1; reconciled in the roadmap's Reviews). `dockhand bump chezmoi` took chezmoi from 2.72.2 to 2.73.0, passed its check, and held for a look on "go.mod adds github.com/dustin/go-humanize v1.1.0". But 2.72.2's go.mod already required it, as `v1.0.1 // indirect`. 2.73.0 had only come to import it directly, one minor version on.

## What was wrong

The comparison read only a go.mod's direct requirements (`sourcecompare`'s `goModules`, and `archive`'s reader before it). So a module that went from indirect to direct read as new, and a new dependency holds the update. Every Go update that promoted a module stopped for a person. The mirror image was quieter: a module that went from direct to indirect read as dropped, though the build keeps it.

## What changed

- **The reader** keeps a go.mod's indirect requirements apart from its own declarations: modules the build already has, for its dependencies' sake. The other manifests have no such kind, and read as before.
- **The comparison** takes neither of those as gained or lost:
  - A promotion or demotion at the same version says nothing, since the build takes the same module.
  - Where the version moves, it is a move, which doesn't hold, marked indirect on that side: "go.mod moves github.com/dustin/go-humanize from v1.0.1 (indirect) to v1.1.0".
  - A module new to the build that the module requires directly still holds, as before.
  - A module that leaves the build is still dropped, and an indirect module on its own still says nothing.

**Not taken:** the review's broader claim, that no go.mod change can need a Portfile edit. A module new to the build can need a library from MacPorts, as a cgo module can, so a new direct one still holds for a look.

## Tests

`TestAGoModuleTheBuildAlreadyHadIsNoAddition` has chezmoi's own case, alongside a demotion that moves, a promotion and a demotion that don't, a new direct module, and indirect modules coming and going. With the old reader, it fails with the hold chezmoi's run saw, and "drops" for both demotions. Seven mutations each fail it.
