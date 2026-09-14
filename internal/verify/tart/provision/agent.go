package provision

import "fmt"

const AgentVersion = "0.14.1"
const agentDigest = "96596675452c8a4eed6f93c86a05b6a1e0c4bd2b0e381931b19ddeee3220eb23"
const agentPath = "/opt/dockhand/bin/tart-guest-agent"

func agentURL() string {
	return "https://github.com/openai/tart-guest-agent/releases/download/v" + AgentVersion + "/tart-guest-agent-darwin-all.tar.gz"
}

func agentPlist(label, mode, directory string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>%s</string></array>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/local/bin:/opt/local/sbin:/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
<key>WorkingDirectory</key><string>%s</string>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
</dict></plist>
`, label, agentPath, mode, directory)
}

func agentInstallScript() string {
	daemon := agentPlist("org.cirruslabs.tart-guest-daemon", "--run-daemon", "/var/empty")
	agent := agentPlist("org.cirruslabs.tart-guest-agent", "--run-agent", "/Users/admin")
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
sudo -n /bin/launchctl bootstrap system /Library/LaunchDaemons/org.cirruslabs.tart-guest-daemon.plist
/bin/launchctl bootstrap gui/$(/usr/bin/id -u) /Library/LaunchAgents/org.cirruslabs.tart-guest-agent.plist
`, agentURL(), agentDigest, agentPath, daemon, agent)
}
