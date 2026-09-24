# MacPorts evaluator compatibility

Dockhand evaluates Portfiles with released MacPorts Base 2.11 and 2.12, and checks the capabilities it uses on those as well. The version is read once the `macports` package is loaded, before `mportinit` reads the host's configuration. An older release is refused with a pointer to `sudo port selfupdate`; a newer family is refused as one this dockhand does not support yet, which leaves status and recorded evidence usable; an unrecognized version is refused. A development build (Base master reports x.y.99) is refused as well: master asks the host compiler and SDK questions from its parent interpreter, where dockhand does not observe them, so preparing with it would rest on answers dockhand never saw ([Base design](macports-base-design-prospective.md), order of work, step 1). The evaluator's own tests can run against one with `DOCKHAND_TEST_BASE_ADAPTER=preview`; no command admits one until the preview adapter exists.

`setup` reports the host Base and Tcl versions, successful startup checks, and whether that exact Base release has a source-review record. Its JSON result includes `host_macports`. Evaluated snapshots retain the observed runtime information. A source-review record does not certify a local installation or its PortGroups.

Startup checks require the native metadata/open/close interfaces and dependency-item access before `mportinit`, and working MacPorts version comparison after it, since master loads `vercmp` only in `mportinit`. Opening a port checks the worker's option access. Missing interfaces stop evaluation with the observed Base version and guidance to check `--prefix`/`port-tclsh` or update Base.

Fetch inspection probes native target/hook registration with an inert temporary target, checks the generated wrapper convention, then reads the selected port's fetch target. The probe never executes a fetch hook, modifies source files, or fetches an archive. It removes its temporary target and commands. Unknown record layouts, wrapper conventions, custom fetch procedures, and unsupported hooks disable automatic archive preparation. Metadata can still be read, and preparation reports the reason and suggests manual preparation. Actual Go PortGroup hook recognition remains structural and separate from Base capability checks.

## Evidence

| Base | Evidence |
| --- | --- |
| 2.12.6 | Evaluator suite and capability-failure tests on installed Base/Tcl 8.6.17, Darwin 25 arm64. |
| 2.11.6 | Same evaluator suite on an isolated build from the local `v2.11.6` tag, Tcl 8.6.16, Darwin 25 arm64. Newer host-based credential-selector tests skip; legacy selector tests pass. |
| 2.12.2–2.12.5, 2.10.7, 2.9.3, 2.8.1 | Earlier source review only; 2.10 and older are refused. |
| 2.12.99 (master) | Every `internal/macports` package, `workflow/preparation`, `upstream`, and `outdated` pass with `DOCKHAND_TEST_BASE_ADAPTER=preview`, Tcl 9.0.4, Darwin 25 arm64, except the parent-side host-access regressions, which skip: master asks compiler and SDK questions in its parent interpreter, unobserved. Refused otherwise. |
| Other versions | No source-review record; refused unless in the 2.11 or 2.12 family. |

These tests cover synthetic Portfiles, source-bound resources, variants, subports, metadata failures, fetch-hook inspection, credentials, and version comparison. They do not certify every current PortGroup or port build on these Base versions.

Run the evaluator suite against another isolated installation with:

```sh
DOCKHAND_TEST_MACPORTS_TCLSH=/path/to/prefix/bin/port-tclsh \
  go test ./internal/macports/eval -count=1 -v

# Base master, which the evaluator admits only with the preview adapter:
DOCKHAND_TEST_BASE_ADAPTER=preview \
DOCKHAND_TEST_MACPORTS_TCLSH=/opt/macports-master/bin/port-tclsh \
  go test ./internal/macports/... -count=1
```

Select an appropriate compiler/SDK when building the older installation. The implementation continues to share one Tcl adapter; a separate version adapter should be introduced only for a demonstrated contract difference.
