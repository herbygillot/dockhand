# Classifying unmodeled platform reads

Roadmap item 5 asked for the concrete inputs behind the remaining platform-coverage refusals to be classified before any new profile dimension was modeled. The pinned controls were abendrot, bun, warzone2100, fldigi, and mrustc.

## What the controls actually read

Profiles exist so that every branch of a version, distfile, or checksum declaration is observed before an edit. The previous scanner refused any read of `os.version`, `macos_version`, `macosx_version`, or `macosx_deployment_target` anywhere in a Portfile. The controls read those values only where no source declaration can depend on them:

| Port | Read | Position |
| --- | --- | --- |
| abendrot | `${macosx_deployment_target}` | environment text in a build-phase `system` command |
| bun | `${macosx_deployment_target}` | environment text in a build-phase `system` command |
| warzone2100 | `${macosx_deployment_target}` | a `reinplace` expression iterated by `foreach` in a post-build hook |
| fldigi | `[vercmp $macosx_deployment_target 10.12] < 0` | an `if` whose body appends configure arguments and environment |
| mrustc | `${configure.compiler}`, `${configure.cxxflags}` | Portfile lines 73 and 151, which trigger compiler selection and flag computation against the installed toolchain |

## Classification

A read is harmless when it only formats a command that cannot name or select a source declaration, or when it guards branches made only of such commands. The scanner now walks the Portfile by position instead of matching command text:

- As an argument of a benign sink (`system`, `reinplace`, `xinstall`, file operations, `ui_*`, `notes`, and the `configure.*`, `build.*`, `destroot.*`, `test.*`, `compiler.*`, `cmake.*`, `meson.*`, and `depends_*` families) the read is text.
- In the conditions of an `if` or the iteration words of a `foreach`, `while`, or `for`, every command in the executed bodies, at any depth, must be a benign sink or a control structure whose bodies are; otherwise the refusal names the command the dimension selects.
- Anywhere else, including `set`, `distname`, `master_sites`, `switch`, and non-literal branch bodies, the refusal names the command that reads the dimension.

Darwin-major and architecture handling is unchanged; a nested `os.major` comparison inside a guarded branch still adds its profiles. mrustc's inputs are not a dimension: the values depend on which compilers and SDK the host has, which the roadmap rules out modeling by implication. The refusal is preserved and now names each recorded host access with its Portfile line, taken from the observation's `dockhand.host-access` events.

## Validation

- Scanner tests cover ten harmless placements and thirteen refused ones, including nested and looped branches, `then`/`elseif`/`else` forms, non-literal bodies, and reads in `variant` and `platform` blocks.
- Native integration tests prepare a port whose deployment-target reads sit in configure environment, a guarded configure branch, and a post-patch `reinplace` (baseline and candidate observed, one archive request), and refuse a port whose distfiles are selected by the deployment target before any download.
- Live, on ports commit `0f8e26f480b`: abendrot, bun, warzone2100, and fldigi moved from unknown to input-found with fetch and checksum association passing. abendrot 1.1.1, warzone2100 4.7.0, and fldigi 4.2.14 are candidate-checked; bun is already current at 1.4.2, so no candidate applies. mrustc reports "platform boundary depends on host state: filesystem state outside the captured ports tree (Portfile:73); … (Portfile:151)".

The portedit suite and the full suite pass.
