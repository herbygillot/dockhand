# shellcheck shell=bash
# The fault kit (the project's plan/prime-time-environment.md): what the
# abnormal rows do to dockhand's surroundings. Each fault is undone by its
# _stop, which the row calls in assert, or the runner's next reset.

# fault_proxy starts faultproxy in a mode, stall, reset, 5xx, or pass, and
# says its address, for HTTPS_PROXY; FAULT_PROXY_PID is its process.
fault_proxy() {
	local mode=$1 bin="$ACCEPT_STATE/bin/faultproxy"
	if [ ! -x "$bin" ]; then
		(cd "${ACCEPT_REPO:?}" && GOFLAGS=-mod=vendor go build -o "$bin" ./tools/acceptance/faultproxy) || return 1
	fi
	"$bin" -mode "$mode" >"$ROW_DIR/faultproxy.addr" 2>"$ROW_DIR/faultproxy.log" &
	FAULT_PROXY_PID=$!
	local i
	for i in 1 2 3 4 5 6 7 8 9 10; do
		[ -s "$ROW_DIR/faultproxy.addr" ] && break
		sleep 0.2
	done
	printf 'http://%s' "$(head -1 "$ROW_DIR/faultproxy.addr")"
}
fault_proxy_stop() { [ -n "${FAULT_PROXY_PID:-}" ] && kill "$FAULT_PROXY_PID" 2>/dev/null; FAULT_PROXY_PID=""; }

# fault_low_disk mounts a sparse disk image of a size, such as 2g, and says
# where; fault_low_disk_fill fills it but for some megabytes.
fault_low_disk() {
	local size=$1 mount="$ROW_DIR/lowdisk"
	mkdir -p "$mount"
	hdiutil create -quiet -size "$size" -type SPARSE -fs APFS -volname dhlowdisk "$ROW_DIR/lowdisk.sparseimage" || return 1
	hdiutil attach -quiet -nobrowse -mountpoint "$mount" "$ROW_DIR/lowdisk.sparseimage" || return 1
	FAULT_LOW_DISK=$mount
	printf '%s' "$mount"
}
fault_low_disk_fill() {
	local leave_mb=${1:-50} free_mb
	free_mb=$(df -m "$FAULT_LOW_DISK" | awk 'NR == 2 {print $4}')
	[ "$free_mb" -gt "$leave_mb" ] && mkfile -n "$((free_mb - leave_mb))m" "$FAULT_LOW_DISK/filler"
}
fault_low_disk_stop() { [ -n "${FAULT_LOW_DISK:-}" ] && hdiutil detach -quiet -force "$FAULT_LOW_DISK"; FAULT_LOW_DISK=""; }

# fault_vm_slots takes the Mac's two VM slots with two running clones of a
# vanilla macOS image, ACCEPT_VANILLA, in the user's own Tart home, as a
# person's own VMs would. It says nothing and does nothing without one.
fault_vm_slots() {
	[ -n "${ACCEPT_VANILLA:-}" ] || return 1
	local n
	for n in 1 2; do
		tart clone "$ACCEPT_VANILLA" "dhaccept-slot-$n" || return 1
		tart run --no-graphics "dhaccept-slot-$n" >"$ROW_DIR/slot-$n.log" 2>&1 &
	done
	sleep 20
}
fault_vm_slots_stop() {
	local n
	for n in 1 2; do
		tart stop "dhaccept-slot-$n" >/dev/null 2>&1
		tart delete "dhaccept-slot-$n" >/dev/null 2>&1
	done
}

# fault_shims makes a directory of PATH shims and says it: each tool named
# hidden, as if not installed, and with old-git, a git that says it's 2.39.
fault_shims() {
	local dir="$ROW_DIR/shims" tool real
	mkdir -p "$dir"
	for tool in "$@"; do
		case "$tool" in
		old-git)
			real=$(command -v git)
			cat >"$dir/git" <<SH
#!/bin/sh
[ "\$1" = --version ] && { echo "git version 2.39.0"; exit 0; }
exec "$real" "\$@"
SH
			;;
		*)
			printf '#!/bin/sh\necho "%s: command not found" >&2\nexit 127\n' "$tool" >"$dir/$tool"
			;;
		esac
		chmod +x "$dir/$tool" "$dir/git" 2>/dev/null || :
	done
	printf '%s' "$dir"
}

# fault_flip changes one byte of a file, at an offset, or its middle.
fault_flip() {
	local file=$1 offset=${2:-} size byte
	size=$(stat -f %z "$file")
	[ -n "$offset" ] || offset=$((size / 2))
	byte=$(dd if="$file" bs=1 skip="$offset" count=1 2>/dev/null | od -An -tu1 | tr -d ' ')
	printf "$(printf '\\%03o' $(((byte + 1) % 256)))" | dd of="$file" bs=1 seek="$offset" conv=notrunc 2>/dev/null
}

# fault_truncate cuts a file to a size in bytes.
fault_truncate() { truncate -s "$2" "$1" 2>/dev/null || python3 -c "import os,sys; os.truncate(sys.argv[1], int(sys.argv[2]))" "$1" "$2"; }
