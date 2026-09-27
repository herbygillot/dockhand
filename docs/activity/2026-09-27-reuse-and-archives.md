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

## Still to do in item 6

- **Recorded inputs per port:** each build records its input identity and the archives it consumed.
- **Per-port reuse,** with planning and `Counts` moving out of the engine.
- **Build archives,** ready for dependents only once transferred and checked.
- **The port reader's evaluation report.**
