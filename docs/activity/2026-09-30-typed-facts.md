# 2026-09-30: the evaluator's facts, read as their types

Item 6's first piece, the code-organization review's finding 27, as its own narrowed remedy has it.

The evaluator computes facts of a port beside its options, such as whether it builds anything, whether its livecheck is MacPorts' own, which PortGroups it loads, and whether its fetch is one a direct download repeats. They travel in `PortInfo.Options` under `dockhand.` and `fetch.` keys, and eleven sites compared them as the strings "0" and "1". They stay in `Options`, which is how fidelity compares them. Every reader now goes through an accessor in `macports` (`facts.go`) that reads the fact as its type, and tells a failed or unread fact apart from a false one:
- `MetadataOnly` and `LivecheckStandard`, read as Tcl booleans, with the evaluator's failure as an error;
- `DeclaresTests` and `PortGroups`, with whether they were read;
- `FetchCredentials`, where an unevaluated one is an error, not "none";
- `ArchiveCompatible`, which gives a custom fetch's reason from the evaluator's typed assessment (`PortInfo.Fetch`), prefixed with the Base that assessed it;
- `BaseVersion`, for the User-Agent a livecheck sends, which Base's own sends;
- `IsLivecheckOption`, the one test of whether an option is a livecheck's, which a stub's subport takes from the stub. Its two uses had differed: the options loop took the evaluator's livecheck readings, while the failures loop didn't.

**A failed probe is said.** Whether a port builds anything was probed in a catch that defaulted to "builds", so a stub whose probe failed was taken for an ordinary port, and fidelity then refused with a misleading reason. The failure is now recorded, and `ResolveStub` says it can't tell, as an unsupported edit.

**The fetch's reason isn't an option.** The evaluator had written a custom fetch's reason into `OptionErrors`, which fidelity compared line number and all, and `CheckPolicy` read. `CheckPolicy` now reads `ArchiveCompatible`, and fidelity compares the fetch's kind, which the compatible flag doesn't tell apart among the kinds a direct download repeats.

Not done: carrying the Base version on the bound probe rather than per port, which the review offered as part of the evaluation report; the accessor reads it where it is, and the report (item 6's fourth piece) is where it moves.

Tests: `TestTheEvaluatorsFactsAreReadAsTheirTypes`, `TestAStubWhoseProbeFailedIsSaid`, `TestAFetchIsComparedByItsKind`, `TestAFailedMetadataProbeIsRecorded` (a Portfile whose distfiles can't be read, so the probe fails and the evaluation doesn't), and the refused-hook test, now reading the reason from `ArchiveCompatible`.

Seven mutations each fail a test.
