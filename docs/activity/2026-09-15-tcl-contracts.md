# Tcl process and RPC contracts

Added focused subprocess tests for output/status preservation, stdin EOF, exclusive ownership, bounded stdout/stderr, cancellation, and termination of an uncooperative child. Real Tcl protocol tests cover Unicode/binary/newline payloads, independent concurrent replies, recoverable Tcl errors, bounded diagnostic noise, malformed headers/lengths/status/delimiters, short frames, process exit diagnostics, and cancellation/handshake teardown. No v1 tests or comments were copied.

Tests exposed acceptance of an invalid reply delimiter and dispatch of already-canceled calls. RPC now rejects malformed terminators and returns pre-dispatch cancellation without sending a request or breaking the usable session. Handshake cancellation now also kills the child during script loading, so a non-reading child cannot block an init write beyond the handshake deadline. Context causes survive loading failures. Protocol policy stays in rpc; process ownership and output limits stay in shell. No dependency or package was added.

Validation: focused Tcl tests passed under the race detector; MacPorts adapter tests passed. Tests use temporary subprocesses and do not change the installed MacPorts tree or run verification jobs. The next queue item remains GitHub missing-run recovery.
