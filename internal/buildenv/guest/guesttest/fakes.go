// Package guesttest is what tests of the guest program share, the guest
// package's and the local provider's: stand-ins for MacPorts' port and
// Xcode's otool, as shell scripts.
package guesttest

// FakeOtool stands for Xcode's otool: it prints a file's -L, -l, or -D
// answer from the file beside it, harbor.links, harbor.loads, or
// harbor.id (named apart, since a Mac's file system folds case).
const FakeOtool = `#!/bin/sh
case "$1" in
  -L) cat "$2.links" ;;
  -l) cat "$2.loads" 2>/dev/null; exit 0 ;;
  -D) cat "$2.id" 2>/dev/null; exit 0 ;;
esac
`

// FakePort stands for MacPorts' port in a guest: it records each command,
// has the ports ACTIVE lists active ("  name @spec (active)" lines), prints
// each target's dependencies from DEPS ("name=dep ..."), and fails the
// phases FAIL names ("phase:port ..."), with MacPorts' closing Error
// lines after the failure's own, or with no Error line at all where QUIET
// is set. Each port's dependencies other than for building, as rdeps
// --no-build has them, come from RDEPS ("name=dep,dep ..."). A port's
// archive is ARCHIVES/<name>, and
// its directory devel/<name>, but for UNRESOLVED, which port can't
// resolve. Each step of a build writes its command to the log, "port
// <arguments>", installing dependencies writes DEPLINES lines more, and
// lint writes LINTSAYS, an Error line lint says and goes on past. A
// phase HANG names ("phase:port ...") sleeps, as a compiler that never
// finishes would, in a process of its own. A port NOARCHIVE names has no
// binary archive, which -b archivefetch fails for. With COMMAND set, a
// failure says FAILSAYS as the tool's line, where it's set. With
// ACTIVEFAILS set,
// listing the active ports fails. With ACTIVEFILE set, it's a registry
// of its own, as a prefix of the person's is: the active ports are that
// file's lines, which deactivating, activating, and installing change, a
// target's install activating it at NEWVERSION. INSTALLED lists the
// installed images of a port, as "-q installed <name>" does. Its port
// snapshot keeps the note it's made with, which --list says as ID 7.
// Where PORTSRC names a macports.conf, it's copied beside PORT_LOG once,
// with the sources.conf it names.
const FakePort = `#!/bin/sh
echo "$*" >> "$PORT_LOG"
if [ -n "$PORTSRC" ] && [ ! -f "$PORT_LOG.conf" ]; then cat "$PORTSRC" "$(awk '/^sources_conf/ {print $2}' "$PORTSRC")" > "$PORT_LOG.conf"; fi
drop() { for name in "$@"; do grep -v "^  $name @" "$ACTIVEFILE" > "$ACTIVEFILE.new"; mv "$ACTIVEFILE.new" "$ACTIVEFILE"; done; }
case "$*" in
  "-N snapshot --create --note "*) all="$*"; echo "${all#-N snapshot --create --note }" > "$DOCKHAND_GUEST_ROOT/snapshot.note"; exit 0 ;;
  "-q snapshot --list") if [ -f "$DOCKHAND_GUEST_ROOT/snapshot.note" ]; then printf '   7  2026-10-25 12:00:00+0000  %s\n' "$(cat "$DOCKHAND_GUEST_ROOT/snapshot.note")"; fi; exit 0 ;;
  "-N snapshot --delete "*) exit 0 ;;
esac
if [ -n "$ACTIVEFILE" ]; then
  case "$*" in
    *"-q installed active"*) cat "$ACTIVEFILE"; exit 0 ;;
    "-N -f deactivate active") : > "$ACTIVEFILE" ;;
    "-N -f deactivate "*) shift 3; drop "$@" ;;
    "-N activate "*) drop "$3"; echo "  $3 $4 (active)" >> "$ACTIVEFILE"; echo "port $*"; exit 0 ;;
    *"install --unrequested "*"subport="*)
      all="$*"; name=${all##*subport=}; name=${name%% *}; drop "$name"; echo "  $name @${NEWVERSION:-9.9_0} (active)" >> "$ACTIVEFILE" ;;
    "-N install --unrequested "*|"-N -d install --unrequested "*)
      for dep in $*; do case "$dep" in -*|install) ;; *) grep -q "^  $dep @" "$ACTIVEFILE" || echo "  $dep @1_0 (active)" >> "$ACTIVEFILE" ;; esac; done ;;
  esac
fi
case "$*" in
  *"-q installed active"*) ;;
  "-q installed "?*) printf '%b' "$INSTALLED"; exit 0 ;;
esac
case "$*" in
  *"-q installed active"*) if [ -n "$ACTIVEFAILS" ]; then echo "Error: registry locked" >&2; exit 1; fi; printf '%b' "$ACTIVE"; exit 0 ;;
  *"-q installed"*) exit 0 ;;
  *"-q contents "*) if [ -z "$CONTENTS" ]; then echo "Error: registry locked" >&2; exit 1; fi; printf '%b' "$CONTENTS"; exit 0 ;;
  "provides "*)
    shift
    for file in "$@"; do
      owner=""
      for entry in $PROVIDES; do [ "${entry%%=*}" = "$file" ] && owner=${entry#*=}; done
      if [ -n "$owner" ]; then echo "$file is provided by: $owner"; else echo "$file is not provided by a MacPorts port."; fi
    done
    exit 0 ;;
  "-q location "*)
    shift 2
    while [ $# -gt 0 ]; do echo "$ARCHIVES/$1"; shift 2; done
    exit 0 ;;
  "-q dir "*)
    shift 2
    for name in "$@"; do
      if [ "$name" = "$UNRESOLVED" ]; then echo "Error: Port $name not found" >&2; exit 1; fi
      echo "$DOCKHAND_GUEST_ROOT/ports/devel/$name"
    done
    exit 0 ;;
  *"echo depof:"*)
    for entry in $DEPS; do
      case "$*" in *"depof:${entry%%=*}") echo "${entry#*=}" | tr ',' ' ' ;; esac
    done
    exit 0 ;;
  "-N -b archivefetch "*)
    for name in $NOARCHIVE; do
      case "$*" in *" archivefetch $name") echo "Error: no archive for $name" >&2; exit 1 ;; esac
    done
    exit 0 ;;
  *"-q info --line --version --revision "*)
    for entry in $DEPVERSION; do
      case "$*" in *" ${entry%%=*}"|*"subport=${entry%%=*}") echo "${entry#*=}" | tr ',' '\t' ;; esac
    done
    exit 0 ;;
  *"-q rdeps --no-build "*)
    for entry in $RDEPS; do
      case "$*" in *"--no-build ${entry%%=*}") echo "${entry#*=}" | tr ',' '\n' | sed 's/^/  /' ;; esac
    done
    exit 0 ;;
esac
case "$*" in
  *" deactivate "*|*" clean "*|*" lint "*|*" install "*|*" fetch "*|*" checksum "*|*" test "*)
    echo "port $*"
    case "$*" in
      "-N -d install --unrequested "*|"-N install --unrequested "*)
        if [ "${DEPLINES:-0}" -gt 0 ]; then yes "a dependency's line" | head -n "$DEPLINES"; fi
        if [ -n "$DEPFAILS" ]; then
          echo "Error: Failed to build $DEPFAILS: command execution failed"
          echo "Error: See /opt/local/var/macports/logs/$DEPFAILS/main.log for details."
          exit 1
        fi ;;
      *" lint "*) if [ -n "$LINTSAYS" ]; then echo "$LINTSAYS"; fi ;;
    esac ;;
esac
for entry in $HANG; do
  phase=${entry%%:*}; port=${entry#*:}
  case "$*" in
    *" $phase "*"subport=$port"*) sleep 60 ;;
  esac
done
for entry in $FAIL; do
  phase=${entry%%:*}; port=${entry#*:}
  case "$*" in
    *" $phase "*"subport=$port"*|*" $phase "*"subport=$port "*)
      if [ -n "$QUIET" ]; then exit 1; fi
      if [ -n "$COMMAND" ]; then
        echo "DEBUG: Executing org.macports.build ($port)"
        echo "${FAILSAYS:-go: github.com/spf13/pflag: cannot find package}"
        echo "Command failed:  cd /work/$port && go build ./..."
        echo "Exit code: 1"
        echo "Error: Failed to build $port: command execution failed"
        echo "Error: See /opt/local/var/macports/logs/$port/main.log for details."
        exit 1
      fi
      echo "Error: Failed to $phase $port: it broke"
      echo "Error: See /opt/local/var/macports/logs/$port/main.log for details."
      echo "Error: Processing of port $port failed"
      exit 1 ;;
  esac
done
exit 0
`
