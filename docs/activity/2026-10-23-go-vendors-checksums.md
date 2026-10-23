# 2026-10-23: A Go update whose go.vendors carries its own checksums plans

Field testing: `update git-credential-oauth` (0.17.2 to 0.18.0) refused on
9bdc25ca and on main 25f16ace. The refusal said the checksums are "set by
the golang 1.0 PortGroup, at _resources/port1.0/group/golang-1.0.tcl:521,
which no Portfile edit changes". The Portfile writes every go.vendors row's
rmd160, sha256 and size, which is the case 32427a61 meant to treat as the
Portfile's own.

## Causes

1. **The checksum rule.** golang 1.0's go.vendors handler appends each row
   in separate commands: its file alone (line 521), then each sum, then its
   size. `portfile.WritesChecksums` refused any declaration that carried no
   sum, and so refused the file-alone append. It now treats a declaration
   that carries no value as the Portfile's, since there's nothing for the
   PortGroup to set. A value it does carry, a size now included, is the
   Portfile's where the Portfile writes it, as before.
2. **The prediction.** With the refusal gone, the update refused with
   "[git-credential-oauth.extract.rename changed]". Where go.vendors lists
   a module, the handler turns extract.rename off and wraps extract.cmd,
   emptying its args. The version step evaluates the Portfile with its
   blocks set aside, so it never sees these. This is the dist_subdir case
   of 1b3899e7 for Go. The block step's prediction now takes from a
   table, `blockSettings`, what each block's PortGroup sets where the
   block lists anything: dist_subdir for the Cargo blocks, and
   extract.rename, extract.cmd, extract.pre_args and extract.post_args for
   go.vendors.

## Result

A scratch run planned git-credential-oauth 0.18.0 against a scratch clone.
Only the version and the main archive's checksums changed, and the
go.vendors block stayed the same, as go2port's output does.

## Tests

- TestGoVendorsChecksumsAreThePortfiles covers a file alone, each sum, and
  a size the Portfile writes or doesn't.
- TestGoDependencyPreparation/vendors-extract sets extract.rename the way
  go.vendors' handler does. It fails without the prediction fix.

## rc5's quick-stage row

rc5 is the person's "Cut rc5" (2026-10-23 12:17Z). Acceptance row C11
plans git-credential-oauth, which the quick stage's pin has at 0.17.2, and
fails on any refusal, as C2 and C3 do for Rust. Replayed on the same
scratch clone: rc4 refused mailpit 1.32.0, git-credential-oauth 0.18.0,
and direnv 2.38.2 with the PortGroup sentence, and main planned mailpit
and git-credential-oauth.

Along the way:
- **opkssh** refuses on both builds: its evaluation goes over the RPC
  frame limit of 16 MiB.
- **direnv** on main refuses because its go.mod module is
  `github.com/direnv/direnv/v2`. Go's major-version suffix isn't stripped
  before the comparison with go.package, a check since 5fc664ac.

Both went to prioritization.

## rc5's other two: direnv's major version, and opkssh's frame

- **direnv:** its go.mod module is `github.com/direnv/direnv/v2`, while
  go.package is `github.com/direnv/direnv`. Dependency regeneration
  compared the whole paths and refused it as a module that moved. depblock's
  `samePackage` now drops the module's major-version suffix (/vN, or
  gopkg.in's .vN) with golang.org/x/mod's `module.SplitPathVersion`. A
  go.package that names its own major version still has to match exactly.
  assess's moduleMoved still compares whole paths, so a release that moves
  to a new major still holds for a look. direnv 2.38.2 plans.
- **opkssh:** measured first. Every declaration the evaluation records
  carries its source frames, and each frame carries its command's text.
  Every checksums-append that go.vendors makes had the whole go.vendors
  command on its stack, so a block of n rows sent n copies of an n-row
  command. opkssh's ~500 lines came to 18.5 MB, past the 16 MiB RPC frame
  limit. The limit stays. observation.tcl now sends each frame once, in a
  table the events name by index, and eval decodes it. opkssh 0.19.1
  plans. caddy, at 881 lines, now evaluates, and is refused by the
  designed rule for a patch that edits its manifests.
  TestALongListsFramesAreSentOnce's 600 rows sent 34 MB before the fix.
- Acceptance rows C12 (direnv) and C13 (opkssh) plan them at the quick
  stage's pin.
