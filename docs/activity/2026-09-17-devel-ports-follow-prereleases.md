# Devel ports follow prereleases

`bump git-devel` stopped with "automatic selection does not support this source convention: require a stable numeric version". The subport rides `2.56.0-rc0` on purpose, and upstream had `v2.56.0-rc1`; the stable-only rule that protects ordinary ports was the wrong rule for it. Getting the bump to a diff took four changes, each of which the port exposed in turn.

## Prerelease ports follow prereleases

Automatic selection now classifies the current version with `releasever`. A stable current version admits stable candidates only, exactly as before. A prerelease current version admits prereleases as well, still through the port's own livecheck filter and MacPorts version ordering, so a `-devel` port moves from one release candidate to the next and to a final release when that compares newer. Draft releases are never candidates, and a current version the classifier cannot place, such as a patch-letter spelling, still needs an explicit version, with the message now saying "stable or prerelease". Moving between prereleases does not count as leaving stable, so no warning is printed.

## Tags that name no commit are skipped

git/git carries `junio-gpg-pub`, a tag on a GPG key blob, for which GitHub reports no commit. The tag listing refused the whole repository over it. Such a tag can never be a release, so the listing now skips tags without a commit or with an unusable ref name instead of aborting discovery.

## Conditional rejection guards are recognized

The perl5 PortGroup registers a `pre-fetch` hook that errors when a required variant is missing. The fetch-semantics check accepted unconditional rejections and the Go toolchain guard and refused everything else. It now also recognizes hooks made only of `if` statements whose conditions read variables and whose every branch is a rejection, reported as guards that "only reject unsupported configurations". Conditions with command substitutions, and branches that do anything else, are still refused.

## Compiler-selection probes are explained by build-only reads

On the modeled Darwin 10 profile that git's `os.major` branches require, `set CFLAGS "${configure.cflags} …"` made MacPorts run compiler selection, which probes the host for compilers the profile lacks, and the evaluation was refused as depending on host state. Reads of `configure.compiler`, the compiler variables, and the flag options now form a second classified dimension beside the minor-version reads: harmless in build-only positions, refused where they can reach a source declaration. A variable set from such a read carries it to its later uses, a `set` inside a guarded branch taints rather than refuses, loop variables bound from the dimension carry it, post-extraction hooks inside a guarded branch are judged by their bodies, and file-handle commands join the benign sinks. When a modeled evaluation records host accesses that all originate in `portconfigure::` and the Portfile's toolchain reads are all benign, the probes are removed from the observation's problems at the three sites that read them; any other host access is still refused, with its Portfile line named.

## Validation

- Selection tests cover a port on `4.0-rc1` selecting `4.0-rc2`, a stable port ignoring the same prerelease tags, and the unknown-spelling refusal; a tag-listing test skips a blob tag and a bad ref name; guard tests accept the perl5 shape and `elseif`/`else` forms and refuse command substitutions, extra commands, and non-rejecting branches; scanner tests cover toolchain reads in build positions, through variables, and inside hooks, with counterexamples reaching `distname`, `master_sites`, and `distfiles`.
- Live, `bump git-devel --diff` selects 2.56.0-rc1 and previews the version and checksum edits with no stability warning. `assess` now reports abendrot, bun, warzone2100, fldigi, mrustc, and git-devel as input-found; mrustc was the last refused control of the platform-coverage item.
