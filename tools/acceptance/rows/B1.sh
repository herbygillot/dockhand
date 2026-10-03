# stages: quick full
# B1: step by step on the small Go port: update, check, tidy, and
# submit's preview. The quick stage's fork is local, so submit stops at its
# preview, which must say so and push nothing.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

act() {
	dh_json update "$(port)" --new || return 0
	dh_json check -p "$(port)" || return 0
	dh_json tidy -p "$(port)" -y || return 0
	dh_json submit -p "$(port)" --plan || :
}

# b_step says why a step of a stepwise row went wrong, or nothing.
b_step() {
	local file=$ROW_DIR/json/$1.json name=$2
	if [ ! -f "$file" ]; then
		printf '%s never ran' "$name"
	elif [ "$(cat "$file.exit")" != 0 ]; then
		printf '%s exited %s: %s' "$name" "$(cat "$file.exit")" "$(jq -r '.error // empty' "$file")"
	fi
}

assert() {
	local why
	for step in "1 update" "2 check" "3 tidy"; do
		# shellcheck disable=SC2086
		why=$(b_step $step)
		if [ -n "$why" ]; then
			row_fail "$why"
			return
		fi
	done
	why=$(b_step 4 "submit --plan")
	if [ -z "$why" ]; then
		row_pass "updated, checked, tidied into one commit, and previewed"
	elif printf '%s' "$why" | grep -qi fork; then
		row_pass "updated, checked, tidied, and stopped at submit's preview for want of a GitHub fork, which the quick stage hasn't"
	else
		row_fail "$why"
	fi
}
