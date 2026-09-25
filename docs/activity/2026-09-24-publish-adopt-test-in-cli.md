# 2026-09-24: the publish --adopt CLI test lives in cli

Step 1 of the roadmap's Next, the last item carried from the review
follow-up. `TestPublishCLIAdoptsManualBranchOnlyAfterDryRun`, the only
end-to-end CLI test of `publish --adopt`, lived in `workflow` and drove
`cli.Run` with production wait intervals; at about 32 seconds it was the
slowest test in the tree. It moved to `cli/combined_publication_test.go`,
where `runFixture` shortens the intervals, and runs in about 2.6 seconds.

It is rebuilt on `cli`'s own fixtures rather than `workflow`'s: a
hand-made `manual` branch one commit above master, a standalone passing
verification of it (`seedCLIVerification`), and `publicationCLI`'s fake
GitHub and fork remote. It asserts what the old one did: the dry run
plans the branch's head with the recorded evidence and tracks, pushes, and
writes nothing; publishing without a token is refused with
`github.ErrAuthentication` and accepts no job; and the real run publishes,
creating one pull request, on the recorded evidence with no new attempt.
`workflow/publication_cli_test.go` is removed, and with it `workflow`'s
test dependency on `cli`.
