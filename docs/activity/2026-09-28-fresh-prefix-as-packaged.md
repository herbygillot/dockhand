# 2026-09-28: the fresh prefix is MacPorts' package's

The first CI run with MacPorts installed ([MacPorts in CI](2026-09-28-macports-in-ci.md)) failed one test, `TestTheInstallationIsFresh`: in the fresh installation Portfiles are evaluated against (oracle phase 4), `${prefix}/lib/pkgconfig` wasn't a directory. On this Mac it was.

**Why.**
- The fresh model says a prefix holds Base's own files and the skeleton Base's installer makes from its `prefix.mtree` and `base.mtree`.
- The dispatcher answers a question about a skeleton directory from the host's prefix, taking the host to have it.
- This Mac has `lib/pkgconfig` because its ports put files there. CI's MacPorts, fresh from its package, doesn't.

**What a fresh installation has.** MacPorts is installed from its package, in dockhand's images, in MacPorts CI, and now in dockhand's CI. The 2.12.6 package's payload, listed with `pkgutil --payload-files`, has 128 of the skeleton's 135 directories. It leaves these for ports, or for MacPorts itself, to make later:
- `lib/pkgconfig`;
- `var/log`;
- `www`;
- `var/macports/logs` and `var/macports/software`;
- `var/macports/home/Library` and its `Preferences`.

It also installs `bin/portf`, which the model's list of Base's programs left out.

**The model now follows the package.** Those seven directories are left out of the skeleton, and `portf` is one of Base's programs. A question about one of the seven is answered as absent on every host, so this Mac and a fresh one agree. The 128 are on every host installed from the package or from source.

**The test** now checks both sides: `lib` is there, and `lib/pkgconfig` isn't, whatever this Mac has. It fails with the seven put back.
