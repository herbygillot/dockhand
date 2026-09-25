package provision

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/tart"
)

func agentURL() string {
	return "https://github.com/openai/tart-guest-agent/releases/download/v" + tart.GuestAgentRelease + "/tart-guest-agent-darwin-all.tar.gz"
}

// agentPlist describes one of the guest agent's services. A log path, when
// given, keeps what the service says: the daemon grows the guest's disk
// with --resize-disk, and without a log its failures were silent.
func agentPlist(label, mode, directory, log string) string {
	logging := ""
	if log != "" {
		logging = fmt.Sprintf("<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>\n", log, log)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>%s</string></array>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/local/bin:/opt/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
<key>WorkingDirectory</key><string>%s</string>
%s<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
</dict></plist>
`, label, tart.GuestAgentPath, mode, directory, logging)
}

// agentDaemonLog is where the guest agent's daemon, and its disk resize,
// log inside the guest.
const agentDaemonLog = "/var/log/tart-guest-daemon.log"

func agentInstallScript() string {
	daemon := agentPlist("org.cirruslabs.tart-guest-daemon", "--run-daemon", "/var/empty", agentDaemonLog)
	agent := agentPlist("org.cirruslabs.tart-guest-agent", "--run-agent", "/Users/admin", "")
	return fmt.Sprintf(`set -eu
archive=/tmp/tart-guest-agent.tar.gz
/usr/bin/curl -fsSL -o "$archive" %s
printf '%s  %%s\n' "$archive" | /usr/bin/shasum -a 256 -c -
sudo -n /bin/mkdir -p /opt/dockhand/bin
sudo -n /usr/bin/tar xzf "$archive" -C /opt/dockhand/bin tart-guest-agent
sudo -n /bin/chmod 0755 %s
/bin/rm -f "$archive"
sudo -n /usr/bin/tee /Library/LaunchDaemons/org.cirruslabs.tart-guest-daemon.plist >/dev/null <<'DOCKHAND_DAEMON'
%sDOCKHAND_DAEMON
sudo -n /usr/bin/tee /Library/LaunchAgents/org.cirruslabs.tart-guest-agent.plist >/dev/null <<'DOCKHAND_AGENT'
%sDOCKHAND_AGENT
sudo -n /usr/sbin/chown root:wheel /Library/LaunchDaemons/org.cirruslabs.tart-guest-daemon.plist /Library/LaunchAgents/org.cirruslabs.tart-guest-agent.plist
sudo -n /bin/chmod 0644 /Library/LaunchDaemons/org.cirruslabs.tart-guest-daemon.plist /Library/LaunchAgents/org.cirruslabs.tart-guest-agent.plist
%s
`, agentURL(), tart.GuestAgentDigest, tart.GuestAgentPath, daemon, agent, agentRegistrationScript())
}

func agentRegistrationScript() string {
	return `set -eu
launch() {
  if [ "$domain" = system ]; then sudo -n /bin/launchctl "$@"; else /bin/launchctl "$@"; fi
}
register_service() {
  domain=$1
  plist=$2
  label=$3
  attempt=0
  detail='launchd domain is not available'
  printf 'Registering %s in %s...\n' "$label" "$domain"
  while [ "$attempt" -lt 120 ]; do
    if launch print "$domain" >/dev/null 2>&1; then
      if launch print "$domain/$label" >/dev/null 2>&1; then return 0; fi
      bootstrap_status=0
      detail=$(launch bootstrap "$domain" "$plist" 2>&1) || bootstrap_status=$?
      if launch print "$domain/$label" >/dev/null 2>&1; then return 0; fi
      if [ "$bootstrap_status" -ne 125 ]; then
        printf 'Unable to register %s in %s (exit %s): %s\n' "$label" "$domain" "$bootstrap_status" "$detail" >&2
        return 1
      fi
    fi
    attempt=$((attempt + 1))
    /bin/sleep 1
  done
  printf 'Timed out registering %s in %s: %s\n' "$label" "$domain" "$detail" >&2
  return 1
}
register_service system /Library/LaunchDaemons/org.cirruslabs.tart-guest-daemon.plist org.cirruslabs.tart-guest-daemon
register_service gui/$(/usr/bin/id -u) /Library/LaunchAgents/org.cirruslabs.tart-guest-agent.plist org.cirruslabs.tart-guest-agent
`
}
