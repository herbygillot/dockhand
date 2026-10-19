# A hook that edits a manifest, over the lock's own block

A user's report (Link Dupont, macports-dev, editors/fresh), at 6dd7745c:
`update fresh 0.5.3 --plan` refused, "post-patch edits a dependency
manifest; regenerate manually", over fresh's post-patch, which drops the
self-update default feature from crates/fresh-editor/Cargo.toml, and its
way on named the archive's checksums alone, so the person regenerated
some 700 cargo.crates lines by hand.

Taken as the coordinator's counter-proposal had it, against reading each
reinplace for a removal, where a removal and an addition can look alike:
the evidence is the Portfile's own block. A pre-patch, post-patch, patch,
post-extract, or pre-configure hook that edits a manifest
(`sourceInput.manifestHook`) no longer refuses at once. The update reads
the current version's lock, as it does to find maintained overrides; where
the declared block is exactly what that lock gives, as fresh's 717 crates
were 0.5.1's whole Cargo.lock, the hook shaped nothing, and the block is
regenerated from the new version's lock as the maintainer did. Where it
differs, or couldn't be compared, the update holds, its way on naming the
block's regeneration (`manifestHookRefusal`: "regenerating cargo.crates
with cargo2port from the new version's source", or go.vendors with
go2port). TestGoDependencyPreparation's patched and patched-curated
scenarios.
