# 2026-09-24: the last raw Tcl boolean readers

Step 1 of the roadmap's Next, carried from the review follow-up. The
2026-09-23 architecture review found `use_xcode` read three ways and made
`macports.PortInfo.Bool` the one reader of Tcl booleans (yes, true, on, 1
and no, false, off, 0, in any case). Four sites still compared strings:

- `go.offline_build` (`portedit/go_toolchain.go`): module mode was
  `== "no"`, so `No` or `off` read as GOPATH mode. It is now the option
  set and false; unset or unreadable leaves the minimum alone, as before.
- `cargo.update` (`portedit/dependencies.go`, now `checkCargoUpdate`): a
  deny list of `""`, `no`, `false`, `0` let `off` through as an update.
  It is refused when true or not a boolean.
- `fetch.ignore_sslcert` (`portedit/archives/download.go`): archive
  preparation required exactly `no`, refusing `off` and `false`. Any
  spelling of false now passes; true, a non-boolean, or an option never
  evaluated is refused, as before.
- The livecheck helper (`macports/source/livecheck.go`): its own
  `boolean` list, which also refused an unset option, is gone;
  `livecheck.ignore_sslcert` and `livecheck.compression` go through
  `Bool`, still refused as unsupported when not booleans.

Tests pin each site's spellings (`TestCheckPolicyReadsIgnoreSSLCertAsTclBoolean`,
`TestModuleModeReadsOfflineBuildAsTclBoolean`,
`TestCargoUpdateIsReadAsTclBoolean`, and two cases in the livecheck
table).
