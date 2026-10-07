# stages: full
# D-C6: Git's own push credentials missing: an HTTPS fork remote with no
# credential helper, and an SSH key GitHub doesn't know, offered alone
# with no agent. Each fails naming git's credentials, and never hangs.
# The key is made by the row and has a passphrase, but isn't on the test
# account, so the case is an unknown key's refusal; a passphrase-locked
# key GitHub knows isn't covered (the rc8 full stage).
#
# The row sets both up itself, in its own scope, and puts them back: the
# clone's push URL on HTTPS with an empty credential.helper in its local
# config, which resets the system's osxkeychain, as a global unset didn't;
# and a throwaway key with a passphrase, offered alone through the row's
# own GIT_SSH_COMMAND, with no agent, since full.sh's names the test key
# (the rc6 full stage). Its submit names a branch of its own, and is
# --no-check, since setup makes no check: the rc8 preview held for want of
# one before any push, and the credentials were never reached. Its words
# are a test pull request's, should a push ever get through.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	host_only "the fork's push credentials" || return 0
	dh_setup update "$(port)" --new || return 1
	DC6_BRANCH=$(own_branch)
	dh tidy -b "$DC6_BRANCH" -y || return 1
	git -C "$MACPORTS_TREE" remote get-url --push origin >"$ROW_DIR/push-url" || return 1
}
act() {
	host_only "the fork's push credentials" || return 0
	[ -n "${DC6_BRANCH:-}" ] || return 0
	local fork words=(--no-check --title "[testing] $(port): a dockhand credentials test" --skip-notification
		--note "This pull request tests a dockhand release candidate, ${ACCEPT_CANDIDATE:-}, and will be closed without merging.")
	fork=$(git -C "$MACPORTS_TREE" remote get-url origin | sed -E 's#^(git@github.com:|https://github.com/)##; s#\.git$##')
	# An HTTPS push with no credential helper.
	git -C "$MACPORTS_TREE" remote set-url --push origin "https://github.com/$fork.git"
	git -C "$MACPORTS_TREE" config --local credential.helper ""
	printf '$ dockhand submit -b %s -y --no-check   # HTTPS, no credential helper\n' "$DC6_BRANCH" >>"$ROW_DIR/out.log"
	with_timeout 300 "$DH_BIN" submit -b "$DC6_BRANCH" -y "${words[@]}" </dev/null >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	dc6_restore
	# SSH with a key GitHub doesn't know, offered alone, and no agent.
	ssh-keygen -q -t ed25519 -N "dockhand acceptance $$" -C "dockhand acceptance D-C6" -f "$ROW_DIR/passphrase_key" || return 0
	printf '$ dockhand submit -b %s -y --no-check   # SSH, a key GitHub doesn'"'"'t know, no agent\n' "$DC6_BRANCH" >>"$ROW_DIR/out.log"
	(
		unset SSH_AUTH_SOCK SSH_ASKPASS
		export GIT_SSH_COMMAND="ssh -i $ROW_DIR/passphrase_key -o IdentitiesOnly=yes -o IdentityAgent=none"
		with_timeout 300 "$DH_BIN" submit -b "$DC6_BRANCH" -y "${words[@]}" </dev/null >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	)
	rm -f "$ROW_DIR/passphrase_key" "$ROW_DIR/passphrase_key.pub"
}

# dc6_restore puts the push URL and credential helpers back as they were.
dc6_restore() {
	[ -s "$ROW_DIR/push-url" ] && git -C "$MACPORTS_TREE" remote set-url --push origin "$(cat "$ROW_DIR/push-url")"
	git -C "$MACPORTS_TREE" config --local --unset-all credential.helper 2>/dev/null || :
}
teardown() {
	dc6_restore
	rm -f "$ROW_DIR/passphrase_key" "$ROW_DIR/passphrase_key.pub"
	return 0
}
assert() {
	grep -q '^\[exit 142\]' "$ROW_DIR/out.log" && { row_fail "a push hung"; return; }
	grep -q 'git has no credentials for this HTTPS remote' "$ROW_DIR/out.log" ||
		{ row_fail "the HTTPS push didn't say git has no credentials for it"; return; }
	grep -qiE 'SSH server refused|is your key loaded|passphrase' "$ROW_DIR/out.log" ||
		{ row_fail "the SSH push didn't name its key"; return; }
	row_pass "each failed naming git's credentials, and neither hung"
}
