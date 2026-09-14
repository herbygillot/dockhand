# Close submission identities before replacing them

The guest-agent validation pass exposed an intermittent workflow failure in TestCycleClosedSubmissionRetryOrPartialCleanup. Repeating the existing test reproduced it twice in 30 runs. Execution persistence iterated its submissions map directly, so a replacement could be inserted before the old identity's closure was written. SQLite's single-open-submission constraint correctly rejected and rolled back that transaction; the transition remained pending.

Changed execution persistence to write closures before open submissions. The same transaction retains atomicity and the existing uniqueness constraint. Strengthened the existing lifecycle regression to assert that successful replacement and admission cycles report no problems, in addition to checking preserved closure history and the new identity. No provider behavior or database schema changed.

The corrected regression passed 100 consecutive runs. The initially uncertain submission and partial-provisioning case still retain their expected diagnostic outcomes; only successful replacement/admission must be problem-free.

Final validation passed: `go test ./... -count=1`, `go vet ./...`, and `make build`. The working binary was rebuilt.
