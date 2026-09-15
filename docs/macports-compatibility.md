# MacPorts evaluator compatibility

Dockhand checks capabilities, rather than accepting or rejecting an installation solely by Base version. `setup` reports the host Base and Tcl versions, successful startup checks, and whether that exact Base release has a source-review record. Its JSON result includes `host_macports`. Evaluated snapshots retain the observed runtime information. A source-review record does not certify a local installation or its PortGroups.

Startup checks require the native metadata/open/close interfaces, dependency-item access, and working MacPorts version comparison. Opening a port checks the worker's option access. Missing interfaces stop evaluation with the observed Base version and guidance to check `--prefix`/`port-tclsh` or update Base.

Fetch inspection probes native target/hook registration with an inert temporary target, checks the generated wrapper convention, then reads the selected port's fetch target. The probe never executes a fetch hook, modifies source files, or fetches an archive. It removes its temporary target and commands. Unknown record layouts, wrapper conventions, custom fetch procedures, and unsupported hooks disable automatic archive preparation. Metadata can still be read, and preparation reports the reason and suggests manual preparation. Actual Go PortGroup hook recognition remains structural and separate from Base capability checks.

## Evidence

| Base | Evidence |
| --- | --- |
| 2.12.6 | Evaluator suite and capability-failure tests on installed Base/Tcl 8.6.17, Darwin 25 arm64. |
| 2.11.6 | Same evaluator suite on an isolated build from the local `v2.11.6` tag, Tcl 8.6.16, Darwin 25 arm64. Newer host-based credential-selector tests skip; legacy selector tests pass. |
| 2.12.2–2.12.5, 2.10.7, 2.9.3, 2.8.1 | Earlier source review only. |
| Other versions | No source-review record; capability checks still run. |

These tests cover synthetic Portfiles, source-bound resources, variants, subports, metadata failures, fetch-hook inspection, credentials, and version comparison. They do not certify every current PortGroup or port build on these Base versions.

Run the evaluator suite against another isolated installation with:

```sh
DOCKHAND_TEST_MACPORTS_TCLSH=/path/to/prefix/bin/port-tclsh \
  go test ./internal/macports -count=1 -v
```

Select an appropriate compiler/SDK when building the older installation. The implementation continues to share one Tcl adapter; a separate version adapter should be introduced only for a demonstrated contract difference.
