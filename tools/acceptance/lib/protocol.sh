# shellcheck shell=bash
# The full stage's protocol (the project's plan/prime-time.md and
# plan/acceptance-harness.md, H7): human checkpoints, the pull request
# approval list, test pull requests, and the steps only the test host can
# take. In the quick stage and in a dry run (ACCEPT_DRY=1), a row stops at
# its first checkpoint or host-only step as "not run", saying which, so a
# dry run walks every full-stage row up to there.

# protocol_live says whether this run takes the host's steps: the full
# stage, and not a dry run.
protocol_live() { [ "${ACCEPT_STAGE:-}" = full ] && [ "${ACCEPT_DRY:-0}" != 1 ]; }

# host_only marks the row's first step that needs the test host. A dry
# run or the quick stage stops the row there.
host_only() {
	protocol_live && return 0
	row_result "not run" "host only, from: $*"
	return 1
}

# checkpoint stops for a person: WAITING: <what to do>, on the runner's
# own output (the run's log, through fd 3, which run.sh opens on it; a
# row's own output goes to its runner.log, where A1's WAITING sat two
# hours unseen on the rc1 full stage), in the row's runner.log, and in
# $ACCEPT_STATE/waiting, until resume.sh answers done, skip, or fail. A
# skip, a dry run, or the quick stage stops the row as not run; a fail
# fails it.
checkpoint() {
	local what=$* answer
	if ! protocol_live; then
		row_result "not run" "waits on a person, from: $what"
		return 1
	fi
	rm -f "$ACCEPT_STATE/resume"
	printf '%s\t%s\n' "${ROW_ID:?}" "$what" >"$ACCEPT_STATE/waiting"
	say "WAITING: $ROW_ID: $what (tools/acceptance/resume.sh done, skip, or fail)"
	{ printf 'WAITING: %s: %s (tools/acceptance/resume.sh done, skip, or fail)\n' "$ROW_ID" "$what" >&3; } 2>/dev/null || :
	printf 'WAITING: %s\n' "$what" >>"$ROW_DIR/out.log"
	until [ -f "$ACCEPT_STATE/resume" ]; do sleep 5; done
	answer=$(cat "$ACCEPT_STATE/resume")
	rm -f "$ACCEPT_STATE/resume" "$ACCEPT_STATE/waiting"
	printf 'RESUMED: %s\n' "$answer" >>"$ROW_DIR/out.log"
	case "$answer" in
	done) return 0 ;;
	fail) row_fail "a person found it doesn't hold: $what" ;;
	*) row_result "not run" "skipped at: $what" ;;
	esac
	return 1
}

# judged asks a person whether what the row shows holds, after its
# commands have run, and passes the row on done.
judged() {
	checkpoint "judge whether this holds, from $ROW_DIR/out.log: $*" && row_pass "a person judged it holds: $*"
}

# The pull requests a row means to open are on its header line
# "# prs: <port> test|real", one per port. A test one goes to the sandbox
# DOCKHAND_PULL_REQUESTS names, within the test account's fork; a real one
# to MacPorts. Before the first row, run.sh lists every one in
# $ACCEPT_STATE/prs.intended and checkpoints once: the person changes test
# to real for the few that go to MacPorts, and deletes a line to open
# nothing for it. A
# port may be named as ${ACCEPT_GO_PORT} or ${ACCEPT_RUST_PORT}.

# protocol_list_prs writes the pull requests the rows mean to open.
protocol_list_prs() {
	local out=$1 rowdir=$2 row line
	shift 2
	: >"$out"
	for row in "$@"; do
		sed -n 's/^# prs: *//p' "$rowdir/$row.sh" | while IFS= read -r line; do
			line=${line//'${ACCEPT_GO_PORT}'/${ACCEPT_GO_PORT:-go-port}}
			line=${line//'${ACCEPT_RUST_PORT}'/${ACCEPT_RUST_PORT:-rust-port}}
			[ -n "$line" ] && printf '%s %s\n' "$row" "$line" >>"$out"
		done
	done
}

# pr_approved says whether the person approved this row's pull request
# for a port.
pr_approved() {
	local port=$1
	[ -f "$ACCEPT_STATE/prs.intended" ] && grep -qE "^${ROW_ID:?} $port (test|real)\$" "$ACCEPT_STATE/prs.intended"
}

# pr_kind is test or real, as the approved list says.
pr_kind() { sed -n "s/^${ROW_ID:?} $1 //p" "$ACCEPT_STATE/prs.intended" | head -1; }

# submit_pr opens the pull request for a port's branch, if the person
# approved it: a real update at MacPorts, as it is, and a test one in the
# sandbox, titled [testing], with a note that says it will be closed and
# no maintainer mentioned. It says to the row's allowances that one pull
# request may open.
submit_pr() {
	local port=$1 branch=$2 subject
	shift 2
	host_only "submit opens a pull request for $port" || return 1
	if ! pr_approved "$port"; then
		row_result "not run" "its pull request for $port wasn't approved"
		return 1
	fi
	allow_prs "$(($(cat "$ROW_DIR/allow.prs" 2>/dev/null || echo 0) + 1))"
	allow_push "*$branch*"
	if [ "$(pr_kind "$port")" = real ]; then
		DOCKHAND_PULL_REQUESTS="" dh_json submit -b "$branch" -y "$@"
		return
	fi
	# The sandbox's master is MacPorts' as of the last reset, so a test
	# pull request showed MacPorts' later commits as its own (the rc6 full
	# stage): it's set to MacPorts' first, as reset-user.sh sets it. The
	# sandbox's master is disposable, and gh repo sync --force left one B6's
	# merge put ahead of MacPorts', so the ref is set outright.
	local upstream
	allow_push "* refs/heads/master"
	upstream=$(gh api repos/macports/macports-ports/git/ref/heads/master --jq .object.sha 2>>"$ROW_DIR/out.log" || :)
	[ -z "$upstream" ] || gh api -X PATCH "repos/${DOCKHAND_PULL_REQUESTS:?}/git/refs/heads/master" -f sha="$upstream" -F force=true --silent >>"$ROW_DIR/out.log" 2>&1 || :
	subject=$(git -C "$(dh_quiet path "$branch")" log -1 --format=%s)
	dh_json submit -b "$branch" -y --title "[testing] $subject" --skip-notification \
		--note "This pull request tests a dockhand release candidate, ${ACCEPT_CANDIDATE:-}, and will be closed without merging." "$@"
}

# close_test_pr closes a test pull request once the row has its evidence,
# then cleans its branch with clean --closed, which is row C7's too.
close_test_pr() {
	local port=$1 branch=$2 url
	[ "$(pr_kind "$port")" = test ] || return 0
	url=$(dh_quiet --json status "$branch" | jq -r '.result.branches[0].pull_request.url // empty')
	[ -n "$url" ] && gh pr close "$url" --comment "Closed: a dockhand release candidate's test, recorded." >/dev/null
	allow_change "*"
	allow_ref_gone "*$branch*"
	dh_json clean --closed -y
}

# dh_quiet runs dockhand for a value a row reads, not for the row's log.
dh_quiet() { "$DH_BIN" "$@" 2>/dev/null; }

# The test account's login, as dockhand keeps it in the Keychain
# (github.CredentialKey), kept for a row that logs out to test what
# follows, and put back after, so no person enters a code for it: two
# codes expired while D-C2 waited for one (the rc6 full stage). It's held
# in a shell variable alone, never a file or the row's log, and put back
# through security's standard input, as dockhand stores it.
DH_LOGIN_SERVICE=github.com/herbygillot/dockhand
DH_LOGIN_ACCOUNT=github.com

# login_keep holds the Keychain's login in DH_KEPT_LOGIN; it fails where
# there's none.
login_keep() {
	DH_KEPT_LOGIN=$(security find-generic-password -a "$DH_LOGIN_ACCOUNT" -s "$DH_LOGIN_SERVICE" -w 2>/dev/null) || DH_KEPT_LOGIN=""
	[ -n "$DH_KEPT_LOGIN" ]
}

# login_restore puts the kept login back, as it was.
login_restore() {
	[ -n "${DH_KEPT_LOGIN:-}" ] || return 1
	printf 'add-generic-password -U -a %s -s %s -w %s\n' "$DH_LOGIN_ACCOUNT" "$DH_LOGIN_SERVICE" "$DH_KEPT_LOGIN" | security -i >/dev/null 2>&1
}

# device_login logs dockhand in by GitHub's device flow, asking first: the
# code is issued only once the person says they're there, since one
# issued to an empty room expires (the rc6 full stage), then shown at a
# second checkpoint, while setup github waits for it.
device_login() {
	checkpoint "say ready for a GitHub login code: $*" || return 1
	dh_bg setup github --no-browser
	if wait_for_line "$DH_BG_LOG" 'one-time code: ' 120; then
		checkpoint "authorize dockhand as the test account: $(grep -m1 'one-time code: ' "$DH_BG_LOG"), at $(grep -m1 -oE 'https://[^ ]+' "$DH_BG_LOG")" || {
			kill "$DH_BG_PID" 2>/dev/null
			return 1
		}
	fi
	dh_bg_wait
}

# failpoint_bin builds, once a candidate, dockhand from the candidate's
# source with the acceptance build's failpoints, for the kill rows, which
# the release binary can't stop at a named step: the candidate's tag
# where it is one, else the harness's own checkout. It says the binary.
failpoint_bin() {
	local rev=${ACCEPT_CANDIDATE:-HEAD} out source
	git -C "${ACCEPT_REPO:?}" rev-parse -q --verify "$rev^{commit}" >/dev/null 2>&1 || rev=HEAD
	out="${ACCEPT_STATE:?}/bin/dockhand-failpoints-$(git -C "$ACCEPT_REPO" rev-parse --short "$rev^{commit}")"
	if [ ! -x "$out" ]; then
		source=$(mktemp -d "${ACCEPT_STATE}/failpoint-src.XXXXXX") || return 1
		mkdir -p "$(dirname "$out")"
		if ! { git -C "$ACCEPT_REPO" archive "$rev" | tar -x -C "$source" &&
			(cd "$source" && GOFLAGS=-mod=vendor go build -tags acceptance -o "$out" ./cmd/dockhand); } >>"$ROW_DIR/out.log" 2>&1; then
			rm -rf "$source"
			return 1
		fi
		rm -rf "$source"
	fi
	printf '%s' "$out"
}
