# Test fixture boundaries

The checkout-race fixture now invokes the repository's selected Git executable rather than the system Xcode shim. Shell quoting preserves executable paths.

CLI service construction has an unexported, per-root factory. The publication wait/resume fixture uses real application services with short workflow waiting/retry intervals and process polling. Production timing, public configuration, flags, and retry policy are unchanged. The fixture still exercises both admission and completion and asserts the same publication behavior.

Validation: uncached Git and CLI suites.
