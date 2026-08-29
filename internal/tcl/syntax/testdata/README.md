# Parser fixtures

Pure Tcl only; nothing MacPorts-specific lives here. (The MacPorts corpus —
the parser's production dialect — is `internal/mport/testdata`, which the
harnesses also sweep.)

| Directory | Contents | Source |
|---|---|---|
| `scripts/` | 50 real-world Tcl scripts sampled from MacPorts base (including its vendored Tcl/tcllib) and mpbb, named `<repo>__<flattened-path>`. | `macports/macports-base` @ `c254d86be`, `macports/mpbb` @ `91d8eb5` |
| `tcltests/` | 16 files from Tcl's own test suite, chosen for syntactic interest — `parse.test`, `parseOld.test`, `subst.test`, `list.test`, control-flow and variable tests. Written by Tcl's authors to torture Tcl parsers, which is exactly the job here. | Tcl 8.6.17 `tests/`, via macports-base's vendored copy |

All files are unmodified copies. Base and mpbb are BSD-3-Clause; the Tcl
test suite carries the Tcl license.
