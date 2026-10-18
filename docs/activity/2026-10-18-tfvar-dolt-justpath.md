# Three field-testing findings at 23c5e2aa

- **tfvar: a two-digit major version.** `bump tfvar` refused, "existing
  go.vendors differs … for github.com/apparentlymart/go-textseg/v13": the
  Portfile's row is an older go2port's, spelled without /v13, at the same
  lock and checksums (go2port for tfvar 0.9.0 writes the same row with
  /v13). `sameMajor` already reads such a row as one, but its pattern,
  `/v[2-9][0-9]*$`, missed a major version starting with 1. `goMajor` is
  `/v([2-9]|[1-9][0-9]+)$`. TestAGoModuleWithoutItsMajorSuffixIsTheSameRow.
- **dolt: a network fault upstream is tried again.** check-421 failed on
  macOS 12 in go build on proxy.golang.org's "stream error: stream ID 575;
  INTERNAL_ERROR; received from peer", as the port's failure, at attempt 1
  of 3. The guest (`transient`, guest.tcl) reads a failed step's own part
  of its log, but lint's, the test's, and checksum's, for a line naming a
  URL or a TCP connection with a stream reset, a reset connection, a 502,
  503, or 504, or a TLS handshake timeout, and gives it with the result;
  the Tart provider then ends the execution as infrastructure, which the
  runner tries again, the failure kept with the attempt that met it.
  VerifierProtocol stays; guestPin is updated. TestAFailuresReasonIsTheFailingSteps
  and TestGuestTroubleIsInfrastructure.
- **justpath: a build backend the Portfile doesn't name holds.** 0.1.4
  moved pyproject's build backend from poetry-core to setuptools, and bump
  checked it without holding; it failed, "Backend 'setuptools.build_meta'
  is not available". The comparison says a backend moving
  (`pythonBackend`, beside Cargo's binaries), and the assessment holds it
  unless the Portfile's python.pep517_backend, now read, names the new one
  (`macports.PythonBackend` maps a PEP 517 backend to the PortGroup's
  name), saying what to set it to, or that the PortGroup has no name for
  it. assess.Policy is 22. TestAPythonBackendMovingIsSaid and
  TestAPythonBackendMovingHoldsUnlessThePortfileNamesIt. Its dependencies
  moving, typer and colorama to rich and typer, are counted as before
  (D9); checking them against depends_lib waits for the ledger's finding
  kinds.
