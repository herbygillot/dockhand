# Progress-aware driver cycles

## Change

`proc.Manager` now distinguishes a cycle that advanced at least one job from an idle pass. Targeted attachment and resident execution immediately run another pass after recorded progress, allowing ready preparation, verification, and publication transitions to proceed without an artificial one-second pause between each phase. A pass with no job advancement still waits for the configured interval, preserving bounded provider observation and capacity polling.

The workflow engine already reports durable job changes through `CycleResult.Advanced`, so this change adds no scheduler state, timer contract, or alternate readiness decision. Control application and resource cleanup remain safe with the ordinary interval when they do not also advance a job.

## Validation

The process-manager lifecycle test now uses an hour-long idle interval while advancing through admission and completion, proving that progress bypasses the timer rather than relying on a short test setting. An uncached CLI package run fell from 71.381 seconds immediately before the change to 26.989 seconds after it. Remaining time includes deliberate retry and observation delays as well as integration setup; it is no longer one idle interval per successful state transition. The full repository gates were rerun after the change.
