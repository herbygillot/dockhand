# Manifest sources and auxiliary archives

Dependency preparation uses the evaluated extraction plan to select archive candidates, then confirms the manifest at worksrcdir (including cargo.dir) from actual archive contents. Native extract.rename controls whether a differing top-level directory is acceptable. Ambiguous ownership, missing manifests, absent helpers, and unsupported extraction are explicit refusals.

The baseline/helper comparison remains mandatory. Candidate archive planning still precedes downloads. Unchanged auxiliary archives retain their declarations and checksums and are not downloaded merely to regenerate dependency metadata. Archive selection belongs to macports/distfiles; manifest validation belongs to macports/dependency; portedit coordinates the two.

Validation: workflow/preparation, distfiles, and dependency tests passed, including a Cargo source plus independently pinned V8-style file, exact versus renamed manifest roots, missing extraction sources, existing overrides, helper failures, and no-download refusal controls. Real-source exercise results will be recorded with the completed coverage pass.

Implementation and tests were authored here; no v1 comments or tests were copied.
