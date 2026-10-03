# stages: selftest
# A push of the branch's own fork branch, which the row says it makes.
act() { allow_push 'fork refs/heads/dockhand/*'; git -C "$SELFTEST_CLONE" push -q fork HEAD:refs/heads/dockhand/jq-1.8.1; }
assert() { row_pass; }
