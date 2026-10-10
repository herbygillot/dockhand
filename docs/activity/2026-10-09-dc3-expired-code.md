# D-C3: an expired device code

The rc10 rerun of D-C3 (00:30Z) had its device code expire unused; the row
went on to wait for serve's "logged in again" line and recorded a fail on a
login that never came.

- `device_login` (tools/acceptance/lib/protocol.sh) now asks for a new code
  when setup github says the code expired before it was entered, up to three
  codes, each behind a checkpoint saying why.
- D-C3 waits for serve's return only when the login came in; otherwise it
  records "not run" with setup github's reason, since the failure is the
  harness's, not serve's.
