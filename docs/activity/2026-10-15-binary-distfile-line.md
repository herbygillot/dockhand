# A binary distfile said by its name

Field testing at 5eb101a3 (the batch of #35519 to #35527): update said
"Upstream not compared: 1password-cli ships source-906474379 is a macOS
installer package", the temporary file's name in a sentence that didn't
parse. The classification was right, holding nothing.

- `archive.BinaryError` carries the file and what it is, and `Is(ErrBinary)`.
- Update's pair reading names it for the distfile, and the coverage reason
  reads "1password-cli ships a macOS installer package
  (op_apple_universal_v2.31.0.pkg), with no source to compare".
  TestABinaryDistfileIsSaidByItsName.

The same batch's herdr patch warning, a Git-style patch probed at -p0 where
MacPorts uses -p1, joins the roadmap's golang patch item as one cause: the
probe applies patches with the evaluated `patch.pre_args` and `patch.args`.

## CI on 84937cb4: a second heartbeat test's fixed wait

CI's Intel job failed TestAHeartbeatGoesOnAfterAFailedBeat (internal/coord),
unrelated to the change, as TestKeepAliveBeats had on b1ac0ce2: it gave
KeepAlive 300ms to fail three beats and beat again. It now waits for the
beat and both lines, up to ten seconds; 20 runs under -race pass.
