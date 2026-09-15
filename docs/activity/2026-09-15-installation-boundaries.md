# MacPorts installation and Tart boundaries

Added `macports/installation` to own installer selection/execution and read-only MacPorts version, active-port, and platform observations. Provisioning and verification now share those observations. Installation facts do not decide whether active ports or a toolchain are acceptable; callers retain those policies. Provisioning keeps its explicit Tcl-package and compiler checks. OS-level foreign-package-manager observation lives in `macos`.

Tart image-list parsing is shared on `tart.Client`; callers retain local-image filtering and capacity/recovery decisions. Provisioning's native adapter is divided into VM, guest transport, bootstrap, toolchain, and validation files. Verification configuration, execution records, submission, observation, and release/reconciliation are separate files within the existing Tart provider package. Lock ownership, durable checkpoints, guest protocol, and image replacement lifecycle remain unchanged.

Validation: affected MacPorts, macOS, Tart, provisioning, provider, app, and CLI suites; focused race checks. New tests cover read-only inspection, malformed observations, command versus transport failures, cancellation, and both Tart running-state formats. No live images were changed in this refactor.
