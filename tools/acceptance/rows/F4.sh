# stages: quick full
# F4: Git configured its own way: a global hook that fails, commit
# signing on, and core.autocrlf. dockhand turns hooks and signing off for
# its own commands, so its commits come out unsigned, with no hook run.
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
setup() {
	mkdir -p "$ROW_DIR/hooks"
	printf '#!/bin/sh\necho "the hook ran" >>"%s/hook.ran"\nexit 1\n' "$ROW_DIR" >"$ROW_DIR/hooks/pre-commit"
	cp "$ROW_DIR/hooks/pre-commit" "$ROW_DIR/hooks/commit-msg"
	chmod +x "$ROW_DIR/hooks/"*
	cat >"$ROW_DIR/gitconfig" <<CONFIG
[core]
	hooksPath = $ROW_DIR/hooks
	autocrlf = true
[commit]
	gpgsign = true
[gpg]
	program = /usr/bin/false
[user]
	name = Dockhand Acceptance
	email = acceptance@example.invalid
CONFIG
}
act() {
	(
		export GIT_CONFIG_GLOBAL="$ROW_DIR/gitconfig"
		dh update "$(port)" --new || exit 0
		dh_json tidy -p "$(port)" -y || :
	)
}
assert() {
	local branch
	branch=$(git -C "$MACPORTS_TREE" branch --list 'dockhand/*' --format='%(refname:short)' | head -1)
	if [ -f "$ROW_DIR/hook.ran" ]; then
		row_fail "a global hook ran in dockhand's commit"
	elif [ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" != 0 ]; then
		row_fail "tidy failed under the person's Git: $(jq -r '.error // empty' "$ROW_DIR/json/1.json" 2>/dev/null)"
	elif [ -n "$(git -C "$MACPORTS_TREE" log -1 --format=%G? "$branch" | grep -v '^N$')" ]; then
		row_fail "dockhand's commit is signed, or tried to be"
	else
		row_pass "committed unsigned, with no hook run, under hooksPath, gpgsign, and autocrlf"
	fi
}
