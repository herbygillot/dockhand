# Human-correction workflow design

Documented concrete behavior and proposed command forms for amendment, rebase, and explicit branch reassociation. Ordinary Git editing remains supported; source-tree applicability controls verification reuse. Local branch names and existing PR remote head branches are distinct. Publication retains the associated PR, requires applicable complete coverage, and uses conditional remote updates.

Chose to keep standalone publication's missing-verification behavior explicit: run verify first. Combined correction-and-publish commands will express the full intended destination. Defined recovery/precondition cases and acceptance exercises. This is design only; no placeholder commands were enabled.
