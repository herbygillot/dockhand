# patchcheck reads git's quoted names, and portedit the Portfile by its path

2026-10-27, Then's item 8, its first two parts (the docs dig's I30, and
I44's first part).

- **Quoted names** (patchcheck `targets`, `headerName`, `gitNames`,
  `quotedName`): a name git quotes, for a space, a tab, or a byte past
  ASCII, `"a/my file.c"` or `"a/caf\303\251.c"`, is unquoted as Go reads a
  quoted string with C's escapes, on `diff --git`, `---`, and `+++`
  lines; it was skipped, so the file wasn't extracted and the patch read
  as not applying. Apple's patch, which MacPorts runs, doesn't take git's
  quoted form itself, so such a patch still doesn't apply there: that's
  patch's verdict, as the build's, no longer patchcheck's misreading.
- **The Portfile by its path** (portedit `Result.portfile`,
  `portfileAfter`, `setPortfile`): the dozen sites that took `Files[0]`
  for the Portfile read the edit whose path is a Portfile, so a patch the
  refresh writes beside it, or one a correction deletes, is its own.
- Tests: TestTargets' quoted names; portedit's suite as it was.
