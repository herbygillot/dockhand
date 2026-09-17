# Livechecks resolved through MacPorts' own checkers

Asked for on 2026-09-17 with a design question attached: add pypi discovery, and avoid ending up with dockhand restating what MacPorts already does.

## The design

MacPorts has no pypi-specific code. `port livecheck` resolves a livecheck type into a URL and a regex by sourcing `_resources/port1.0/livecheck/<type>.tcl` from the ports tree; `pypi.tcl` sets the JSON URL and a version regex and then sets the type to `regex`. A defaulted type is chosen from the master sites the same way (`pypi:` sites become `pypi`, `sourceforge:` become `sourceforge`, and so on), with `fallback`, `googlecode`, and `gnu` as the remaining cases.

dockhand's evaluator now does exactly that, inside the worker interpreter, with the same variables `livecheck_main` prepares and the tree's own checker files, and reports the effective `livecheck.type`, `url`, `regex`, and `name` in place of the raw ones, keeping the declared type as `dockhand.livecheck_declared`. Everything downstream is unchanged: `macports/source` still requires a standard regex livecheck for the evaluated version, and `upstream`'s regex discovery fetches the URL and extracts candidates with MacPorts' own regex engine. There is no pypi code in Go, and the next checker type MacPorts adds works without a change here. Resolution failures, such as a fixture tree without `_resources`, keep the raw values.

## The stub

MacPorts disables livecheck on a python subport whose version equals the stub's, because the stub owns it. Since a stub selection now edits its newest subport, the probe borrows the stub's livecheck declarations for that subport; the release is one and the same. `outdated py-urllib3` reports the update, and a versionless `bump py-urllib3` resolves it.

## Exercise

On the ports tree: `outdated py-urllib3 py-idna py-requests` reported 2.7.0 to 2.8.0, 3.18 to 3.20, and current, all "Selected from livecheck"; `bump py-idna --diff` previewed the shared edit with no version given. The evaluator test resolves a `pypi` type through a checker file in a fixture tree; the edit-service test shows the stub's livecheck borrowed by its newest subport.
