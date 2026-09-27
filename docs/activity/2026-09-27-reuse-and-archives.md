# 2026-09-27: reuse and archives

Roadmap item 6, decisions 28 and 44. The first part is environment identity by origin: a result stands for the environment it ran in only while it is that environment. Before this, a check that passed in an image kept counting after the image was made again from a newer vanilla image, or with other tools.

## Environment identity by origin

**Setup pins its source by digest.** `tart.Registry` reads the digest a tag names through the OCI distribution API, the registries' documented interface, with the anonymous bearer token its challenge asks for. Tart documents pulling by tag. Pulling by `@sha256:` works, but only Tart's source shows it, so setup doesn't depend on it. Instead it reads the digest before and after pulling the tag:
- the same digest both times is the image pulled;
- a different one means the tag moved while it was pulled, and setup stops: "setup: … moved from … to … while it was pulled; run setup again";
- a registry that can't say leaves the origin unknown, and setup says so and carries on.

**Setup records each image's origin on the host.** The image manifest (protocol 3) adds the source's digest, the Command Line Tools installed, and `tart.SetupProtocol`. Once the image is adopted, setup writes the manifest to `~/.dockhand/tart-images/<home digest>/<image>.json` (`tart.WriteImageRecord`), beside the lock directory and keyed the same way. A check reads it without starting a VM. The origin is the source digest, the setup protocol, and the MacPorts, tools, and Xcode versions (`ImageManifest.Origin`). It is empty without a digest, as for an image `v2-final`'s setup made and setup copied in.

**Protocols are pinned by content.** `tart.SetupProtocol` covers the provisioning code, and `buildenv/tart.VerifierProtocol` covers `guest.tcl`. A test pins each one to a digest of the code it covers. A change to that code fails the test until its author decides: raise the protocol, when images or verdicts change, which ends reuse of what was built before; or update the pin, for wording.

**The provider says what an environment is.** `buildenv.IdentityProvider` is a new capability. The Tart provider's `Identity` is the image's recorded origin with the verifier protocol, or empty for an image with no origin recorded. The other providers don't implement it yet.

**Each execution records its identity** (schema 15, `executions.identity`), read as the execution begins.

**Evidence compares it with the identity now.** `engine.Counts` gains the rule: a result counts where its check planned the target in that environment, and the environment's identity when the execution began is its identity now. Identities are read before evidence's transaction, since a transaction never calls a provider (`evidenceNow`), for `submit`, `status`, `submit --passing`, and `serve` alike. A result that no longer counts:
- in the latest check, it is dropped to not run, and its environment is noted as remade;
- in an earlier check, it isn't taken, and is noted the same way;
- a later check that built the target in the environment as it is now clears the note.

Three cases keep today's behaviour:
- A provider that can't say what an environment is now keeps its results.
- A result with no execution behind it, an unmet need, is the plan's.
- An execution recorded before this change has no identity. It keeps counting until its image is made again with a recorded origin, since only then does the environment say what it is.

**What a person sees.**
- `submit` refuses: "jq was checked in tart macOS 26 (Tahoe) arm64 with Xcode before it was made again, from another source or with other tools; dockhand check builds it there again, or share the branch as a draft (--draft)".
- `status` says "check-3 passed, but … has been made again since, from another source or with other tools; jq must be built there again", pointing to `dockhand check`.
- `--json` marks the result `remade`.

**Checked live.**
- The registry read gives Tahoe's and Sequoia's vanilla digests in 100 to 200 ms.
- Tahoe's is the digest Tart's own `tart list --source oci` names for the image it pulled.

**Tests.**
- `TestAResultStandsOnlyWhileItsEnvironmentDoes`, with a scripted provider that says its identity:
  - the identity is recorded on each execution;
  - an unknown identity keeps the results;
  - a changed one blocks submit and `--passing`;
  - two narrowed checks in the remade environment clear it.

  It fails with the identity rule disabled.
- `Counts`'s own test adds the identity cases.
- The status row, and the registry against a fake registry with a token challenge, are tested too.
- So are setup's record, the image record's round trip, and the Tart provider's identity.

## Images keep their archives

Decision 28 planned to identify, and later keep, the archives in a guest's `${prefix}/var/macports/software/<port>/`. MacPorts has since changed what it keeps there. `macports.conf` documents `portimage_mode`, whose default on a filesystem that clones, APFS included, is `directory`: "Downloaded archives are extracted to create the directory and then deleted." A guest built that way keeps no archive to identify a dependency by, or to save. This Mac's `/opt/local` shows it: `port location` names a directory for most ports, and a `.tbz2` only for ports installed before the setting existed.

So setup now sets `portimage_mode directory_and_archive` in the image's `macports.conf` (`installation.KeepArchives`). That setting is documented: "Both archives and extracted directories are kept". MacPorts' registry then keeps the archive as the port's location, so `port location` names a file. It is appended once, after the file's own settings, which it overrides.

That changes what an image holds, so `tart.SetupProtocol` is 2. Its pin now covers `macports/installation` as well as `tart/provision`: the MacPorts installer is provisioning code, and the first pin left it out.

## What each build read

Each result now names what its build read (decision 28), as far as dockhand can name it (`model.TargetInputs`):
- the environment, by its identity as the execution began;
- the target's directory, and `_resources`, each by its tree in the revision. All of `_resources` counts as read until evaluation records what it sources;
- the variants asked for;
- every port active as it built, other than itself: its name, and its version, revision, and variants as MacPorts names them (`@1.88.0_3+no_single`). Each comes with where its name resolved in the ports tree and that directory's tree, and its archive's digest.

The result also keeps the digest of the archive its build made, for dependents to be installed from later.

**The guest sees; the host names by content.** After each verdict, the guest asks `port` once for the whole active set:
- `port -q installed active` lists the active ports;
- `port -q location` names each one's archive, which the guest digests with `shasum`, once per archive per run;
- `port -q dir` gives where each name resolves.

All three are `port`'s documented actions. Given the whole active set, each answers in order in about 0.2 s on this Mac's 396 active ports. `dir` fails the whole list for a name it can't resolve, so the guest then asks name by name, and a name that fails resolves nowhere. A build whose inputs can't be read keeps its verdict, with its inputs unknown and a line in its log saying so. Recording adds no step to how ports are built or judged, so `VerifierProtocol` stays 1, re-pinned.

The provider passes what the guest saw through `buildenv.Build.Consumed`. The runner completes it with the revision's trees (`reuse.Inputs`, with `git.Repository.Directories`); a tree is always the revision's, never the provider's. It records the inputs with the result, in one transaction.

**Stored once by content** (schema 16): an `inputs` table keyed by the digest of the record, which the result names; a result can't name inputs that weren't recorded. Inputs are complete when the environment, both trees, and every active port's tree and archive are known (`TargetInputs.Complete`). Only complete inputs will stand for another build's.

**What stays unknown.** The github and command providers don't report active ports yet, so their results have no inputs. On a protocol-1 image, which keeps no archives, the active ports have no digests, so the inputs are incomplete.

**Checked live** on a scratch branch bumping jq, checked in a clone of the Tahoe Xcode image. That image was made before this change: setup protocol 1, with no origin recorded. check-22 passed, and jq's result names inputs that hold:
- the environment's identity, empty, as the image has no origin record;
- `sysutils/jq` and `_resources` by the revision's trees;
- one active port, oniguruma6 `@6.9.10_0`, resolved to `devel/oniguruma6` in the staged tree, `/var` against `/private/var` notwithstanding, with its tree and its archive's digest.

jq's own archive digest is empty. Its log shows why. In `directory` mode MacPorts builds a port into an image directory and never writes an archive (`portinstall.tcl`: "only the extracted dir should be kept"). oniguruma6 came from packages.macports.org as a `.tbz2`, and the registry still names that file. Under `directory_and_archive`, install writes the archive, and activation keeps it beside the directory. So the image's setting is what gives a target its own archive, as setup protocol 2 now does. That part is unchecked live: it needs a new image, which is the person's to make.

**Where it lives.** `internal/reuse` is new, and holds `Inputs`. The reuse decision will move there, and `Counts` with it, rather than grow in the engine (item 4).

## Setup lost what it wrote last

The images rebuilt with setup protocol 2 didn't keep archives. check-23, in the rebuilt Tahoe Xcode image, recorded the environment's identity and oniguruma6's archive, but jq's own archive was still empty, and jq's log showed MacPorts in `directory` mode. A clone of the rebuilt base image showed why:
- its `macports.conf` was byte for byte the default, last written by MacPorts' installer at 20:17:53;
- `/opt/dockhand/image.json`, the manifest setup writes into the guest after that, wasn't there at all.

`KeepArchives`' script, run by hand in the clone, appends the setting. So setup had written both, and they were lost.

The cause is how setup stops the guest. After the manifest, it runs `tart stop`, which asks the guest to shut down and ends it when it doesn't in time. What the guest still held in memory never reached its disk. A throwaway clone showed it plainly:
- a file written just before `tart stop` was gone at the next boot;
- the same write followed by `sudo sync` survived.

Nothing reads the in-guest manifest, so its loss went unnoticed from the start.

Setup now flushes the guest (`sync`) right before stopping it (`Flush`), and a test holds the order. That changes what an image holds, so the setup protocol is 3. Protocol 2's images have origin records but no archives, and are to be made again.

## Still to do in item 6

- **Recorded inputs per port:** each build records its input identity and the archives it consumed.
- **Per-port reuse,** with planning and `Counts` moving out of the engine.
- **Build archives,** ready for dependents only once transferred and checked.
- **The port reader's evaluation report.**
