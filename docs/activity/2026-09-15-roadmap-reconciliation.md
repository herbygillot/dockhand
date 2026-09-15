# Roadmap reconciliation and reassessment

Reconciled the current queue against code, recent reviews, and activity reports. Moved unresolved provisioning findings into the roadmap; recorded completed calculated-version probing, GitHub verification/recovery, retry scheduling, and automatic provider selection. Corrected superseded architecture/component/CLI descriptions and made the durable status contract explicit in the principles. Historical reports and untracked review material are unchanged.

Reassessment: Tcl shell/RPC tests and GitHub missing-run diagnostics remain the next two bounded tasks. Dependent discovery and scheduling exist but their integration still spans per-target configuration and publication coverage; it follows checksum refresh and human-correction design. No new provider, generic scheduler, or account-wide rate-limit service is justified by this pass.

Validation: checked descriptions against current app provider selection, Tcl protocol, GitHub reconciliation, and workflow retry implementations; reviewed the documentation diff and whitespace. Documentation only; no runtime checks needed.
