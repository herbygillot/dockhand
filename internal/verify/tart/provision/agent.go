// Package provision builds the base images the tart verifier clones
// from, starting from a stock vanilla macOS image and adding only what
// dockhand needs: the tart guest agent, and MacPorts.
//
// Vanilla rather than Cirrus Labs' base or xcode images, because those
// install Homebrew — 3.1 GB and some 390 binaries, with /etc/paths.d
// putting it on every user's PATH. An environment that exists to answer
// whether a port declared its dependencies cannot ship another package
// manager, and removing one afterwards is worse than never having it:
// in those images the guest agent is itself a brew formula living in
// the Cellar, so uninstalling Homebrew without rescuing it first yields
// an image that looks fine while running and is unreachable once
// cloned. A vanilla image has none of that.
//
// The cost is one bootstrap problem. Vanilla has no guest agent, so
// `tart exec` does not work until dockhand installs one, and installing
// it needs a way in. That way is SSH, which the vanilla images enable
// during their own build — and it is needed exactly once, here. Every
// later conversation with a guest, provisioning included, goes through
// the agent.
package provision

import (
	"fmt"
	"strings"
)

// AgentVersion is the tart guest agent this installs. It is pinned:
// an image is provisioned once and cloned for months, so "whatever was
// latest that afternoon" is not a property an environment should have.
// Cirrus Labs' own images ship 0.10.0; this is deliberately newer.
const AgentVersion = "0.14.1"

// AgentURL is the official release archive — one universal binary for
// both architectures.
func AgentURL() string {
	return fmt.Sprintf(
		"https://github.com/openai/tart-guest-agent/releases/download/v%s/tart-guest-agent-darwin-all.tar.gz",
		AgentVersion)
}

// AgentDir is where the agent lives: dockhand's own prefix, colliding
// with neither MacPorts at /opt/local nor the Homebrew this image will
// never have.
const AgentDir = "/opt/dockhand/bin"

// agentPath is the installed binary.
const agentPath = AgentDir + "/tart-guest-agent"

// GuestPATH is what every command run through the agent inherits.
//
// Cirrus Labs' plists hardcode /bin:/usr/bin:/usr/sbin:/usr/local/bin:
// /opt/homebrew/bin, which is how a build in one of their images can
// find Homebrew even after MacPorts has sanitized its own binpath.
// Writing the plists ourselves means the guest never names a prefix it
// should not have: MacPorts first, the system after, and nothing else.
const GuestPATH = "/opt/local/bin:/opt/local/sbin:/bin:/usr/bin:/usr/sbin:/sbin"

// launchdJob is one of the agent's two launchd jobs. The daemon serves
// `tart exec`; the agent handles the session-scoped work.
type launchdJob struct {
	Label string
	Flag  string
	Dir   string
	Path  string // where the plist is installed
}

var jobs = []launchdJob{
	{
		Label: "org.cirruslabs.tart-guest-daemon",
		Flag:  "--run-daemon",
		Dir:   "/var/empty",
		Path:  "/Library/LaunchDaemons/org.cirruslabs.tart-guest-daemon.plist",
	},
	{
		Label: "org.cirruslabs.tart-guest-agent",
		Flag:  "--run-agent",
		Dir:   "/Users/admin",
		Path:  "/Library/LaunchAgents/org.cirruslabs.tart-guest-agent.plist",
	},
}

// plist renders a job's launchd definition.
func (j launchdJob) plist() string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
    <dict>
        <key>Label</key>
        <string>%s</string>
        <key>ProgramArguments</key>
        <array>
            <string>%s</string>
            <string>%s</string>
        </array>
        <key>EnvironmentVariables</key>
        <dict>
            <key>PATH</key>
            <string>%s</string>
            <key>TERM</key>
            <string>xterm-256color</string>
        </dict>
        <key>WorkingDirectory</key>
        <string>%s</string>
        <key>RunAtLoad</key>
        <true/>
        <key>KeepAlive</key>
        <true/>
        <key>StandardOutPath</key>
        <string>/tmp/%s.log</string>
        <key>StandardErrorPath</key>
        <string>/tmp/%s.log</string>
    </dict>
</plist>
`, j.Label, agentPath, j.Flag, GuestPATH, j.Dir, j.Label, j.Label)
}

// installAgentScript is the shell run over SSH, once, to make the guest
// reachable by every later means. It is a script rather than a sequence
// of calls because it runs over the one connection dockhand opens.
func installAgentScript() string {
	var b strings.Builder
	fmt.Fprintf(&b, `set -e
curl -fsSL -o /tmp/agent.tar.gz %s
sudo -n mkdir -p %s
sudo -n tar xzf /tmp/agent.tar.gz -C %s tart-guest-agent
sudo -n chmod 0755 %s
rm -f /tmp/agent.tar.gz
`, AgentURL(), AgentDir, AgentDir, agentPath)
	for _, j := range jobs {
		fmt.Fprintf(&b, "sudo -n tee %s >/dev/null <<'PLIST_EOF'\n%sPLIST_EOF\n", j.Path, j.plist())
		fmt.Fprintf(&b, "sudo -n chown root:wheel %s\nsudo -n chmod 0644 %s\n", j.Path, j.Path)
	}
	// Load both now. On a clone these start themselves — RunAtLoad, in
	// the system and session domains — but provisioning has to verify
	// the image before it stops it, and an agent that only appears at
	// the next boot cannot be verified at all.
	//
	// Both, not just the daemon: `tart exec` is served by the session
	// agent, and a guest running only the daemon answers nothing while
	// looking entirely healthy — the process is up, launchctl lists it,
	// and every connection still fails. That cost an hour to find.
	fmt.Fprintf(&b, "sudo -n launchctl load -w %s\n", jobs[0].Path)
	fmt.Fprintf(&b, "launchctl bootstrap gui/$(id -u) %s\n", jobs[1].Path)
	return b.String()
}
