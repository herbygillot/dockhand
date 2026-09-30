# 2026-09-29: checksums dockhand couldn't write

Batch 5 of the roadmap's smaller items, from the hugo exercise's git run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#git-a-port-dockhand-couldnt-update), findings 1 to 4). git fetches three archives from kernel.org, the third, git-htmldocs, from its default `+doc` variant, which declares its checksums with `checksums-append` in its body. dockhand could do none of the update, and the advice it gave led to the same refusal.

## A declaration in a variant's body

MacPorts runs a variant's body as a procedure it builds from the body's text (`makeuserproc`), so Tcl gives its commands no file. A probe of the evaluator's frames showed the stack naming the procedure, `variant-doc`, and then the command by its text. `portfile.LocateDeclaration` now finds such a declaration by that text in the body of the Portfile's `variant doc`, and nowhere else (`locateInVariant`). It refuses a body that reaches its declaration through another procedure, a variant the Portfile defines other than once, and text the body has twice. The frame's line isn't used: it counts from the procedure Base builds, one line longer than the body, and that offset is Base's own detail, not an interface.

## A variant that isn't on by default

As decided today, an update writes every variant's own archives' checksums, not only the default variants'. 37 variants in the tree declare distfiles or checksums of their own and aren't on by default, such as whisper's model sizes. A variant runs its declarations only where it's asked for, so an update and a checksum refresh now ask about each variant whose body in the Portfile declares distfiles, checksums, or master sites (`portfile.ArchiveVariants`, a reading of the Portfile's text only, to choose what to ask MacPorts). Each is observed asked for, on this Mac, through a session whose targets ask for it (`observe.Session.WithVariants`), and its archives are planned, fetched, written, and checked as a platform context's are. An archive the defaults already fetch is covered once. A variant that can't be evaluated asked for refuses the update, saying which, rather than leave its checksums stale. The update's coverage names each variant's context (`ContextCoverage.Variant`).

## Why, in dockhand's words, and advice that works

Where a declaration couldn't be located, `distfiles.Bind` dropped the reason, and the refusal surfaced as "calculated checksum algorithm", wrapped as "baseline {OS:darwin …}: portfile: unsupported source edit: …". The token now keeps why its declaration couldn't be located, and `Bind` returns it as a `distfiles.Unlocated`, naming the archive: "the checksums for doc.zip can't be found in the Portfile to edit: no command written in the Portfile makes it, as an eval'd one isn't". A locator that finds nothing now says so, rather than "ambiguous (0 matches)". It is still an unsupported edit, for everything that reads one.

The command words it without the context it arose in, and gives advice only where it can work. `update` advised `dockhand checksums`, which refused the same way. Now, where checksums can't be located, it says that `checksums` prints them. `checksums`, where it can't write them, fetches every archive of the contexts it observes, a variant's own included, and prints their checksums as a Portfile writes them (`portfile.ChecksumsBlock`, the guide's layout), returned in a `ChecksumsToWrite` beside its reason.

A plan changes nothing, so a refusal of one no longer says "Kept: the branch, unchanged." (finding 4), which a plan from master said of a branch it never started.

## A family's shared checksums

Found after the batch, by the py-flatbuffers addition ([review](../reviews/2026-09-28-hugo-bump-exercise.md#adding-py-flatbuffers-to-35044), finding 1): `dockhand checksums py-flatbuffers` was refused, "evaluation does not match the intended change: … py-flatbuffers.checksums changed". A python stub and its subports share one `checksums` declaration, and a refresh without a release scope let only the selected port's checksums change (`fidelity.Checksums`). Now a sibling of the same version, whose checksums were the selected port's and are its new ones, moves with it. A sibling with checksums of its own, or of another version, is still held to what it was.

Tests:
- `TestADeclarationInAVariantIsLocatedInItsBody`, with the frames as the probe recorded them, and its refusals;
- `TestAVariantDeclaringArchivesIsNamed`;
- `TestAnUpdateWritesADefaultVariantsChecksums`, git's shape, and `TestAnUpdateWritesAVariantsChecksumsWhenItIsntDefault`;
- `TestAChecksumDeclarationNotFoundSaysWhy` and `TestARefreshThatCantWriteReturnsTheChecksums`;
- `TestAChecksumsBlockIsWrittenAsTheGuideLaysItOut` and `TestAnEditDockhandCantMakeSaysWhyAndWhatWorks`;
- the fidelity test's family cases, and `TestARefreshOfAFamilysSharedChecksums`, a stub and two subports.

Seventeen mutations were tried, the family's three included; all but one fail a test. The survivor is the native context's `variant == ""` guard in `applyObservedArchives`, kept on purpose: variant contexts follow the platform contexts, so the first match is a platform's either way, and the guard holds if that order changes.

Not checked live: git is already at its newest release on master, and planning a checksum refresh needs a branch, which a check of this shouldn't start in the person's checkout.
