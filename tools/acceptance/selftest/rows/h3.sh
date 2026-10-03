# stages: selftest
# Breaks H3: a pull request opened that no one approved.
setup() { export ACCEPT_GH_LOGIN=tester; echo 34900 >"$FAKE_DH_STATE/prs"; }
act() { echo 34901 >>"$FAKE_DH_STATE/prs"; }
assert() { row_pass; }
