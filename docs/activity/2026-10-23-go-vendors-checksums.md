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
