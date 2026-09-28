# 2026-09-28: an update dockhand made is an enhancement

The hugo exercise ([review](../reviews/2026-09-28-hugo-bump-exercise.md)) opened macports-ports#35000 with every Type unticked, as #34992 had been. The person said dockhand may tick enhancement itself for a bump or update it makes from scratch.

The description now ticks enhancement when `--type` is not given, every commit carries dockhand's Generated-By line, and one commit's subject is an update edit dockhand recorded on the branch. Tidy writes Generated-By only on a port's commit made of dockhand's own edits, so a commit a person made or changed, an adopted branch, a revbump or checksums branch, and a new port are left for the person to type. `--type` still says what it is and replaces the default. A commit citing a CVE still adds security fix either way.

`TestAnUpdateDockhandMadeIsAnEnhancement` covers the default, `--type` replacing it, and a person's commit on top clearing it. The `--type` flag help and docs/usage.md say what happens without it.
