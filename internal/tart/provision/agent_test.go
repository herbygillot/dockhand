package provision

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentRegistrationRequiresObservedService(t *testing.T) {
	// The script runs in the macOS guest under zsh, as provisioning runs it.
	if _, err := os.Stat("/bin/zsh"); err != nil {
		t.Skip("/bin/zsh is required")
	}
	for _, scenario := range []string{"ready", "domain-delay", "bootstrap-125", "already-loaded", "refused", "never-ready"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			launch := filepath.Join(root, "launchctl")
			require.NoError(t, os.WriteFile(launch, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FIXTURE/events"
key=$(printf '%s' "$2" | tr / _)
if [ "$1" = print ]; then
  case "$2" in
  system|gui/*[0-9])
    if [ "$SCENARIO" = never-ready ]; then exit 1; fi
    if [ "$SCENARIO" = domain-delay ] && [ ! -f "$FIXTURE/domain-$key" ]; then touch "$FIXTURE/domain-$key"; exit 1; fi
    exit 0;;
  *) [ "$SCENARIO" = already-loaded ] || [ -f "$FIXTURE/loaded-${key}" ]; exit $?;;
  esac
fi
if [ "$SCENARIO" = refused ]; then echo 'fixture permission failure' >&2; exit 5; fi
if [ "$SCENARIO" = bootstrap-125 ] && [ ! -f "$FIXTURE/tried-$key" ]; then touch "$FIXTURE/tried-$key"; exit 125; fi
label=$(basename "$3" .plist)
touch "$FIXTURE/loaded-${key}_${label}"
`), 0700))
			script := strings.ReplaceAll(agentRegistrationScript(), "sudo -n /bin/launchctl", "\""+launch+"\"")
			script = strings.ReplaceAll(script, "/bin/launchctl", "\""+launch+"\"")
			script = strings.ReplaceAll(script, "/bin/sleep 1", ":")
			script = strings.ReplaceAll(script, "-lt 120", "-lt 3")
			cmd := exec.CommandContext(t.Context(), "/bin/zsh", "-c", script)
			cmd.Env = append(os.Environ(), "FIXTURE="+root, "SCENARIO="+scenario)
			output, err := cmd.CombinedOutput()
			if scenario == "refused" {
				require.Error(t, err)
				require.Contains(t, string(output), "fixture permission failure")
			} else if scenario == "never-ready" {
				require.Error(t, err)
				require.Contains(t, string(output), "Timed out registering")
			} else {
				require.NoError(t, err, string(output))
			}
			events, err := os.ReadFile(filepath.Join(root, "events"))
			require.NoError(t, err)
			if scenario == "already-loaded" {
				require.NotContains(t, string(events), "bootstrap")
			}
		})
	}
}

func TestLiveAgentRegistration(t *testing.T) {
	name := os.Getenv("DOCKHAND_TEST_BOOTSTRAP_VM")
	if name == "" {
		t.Skip("set DOCKHAND_TEST_BOOTSTRAP_VM to an owned running disposable VM")
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	n := newNative(Config{Executable: "tart", Home: filepath.Join(home, ".tart")}, os.Stdout)
	require.NoError(t, n.BootstrapAgent(t.Context(), name))
	require.NoError(t, n.ReadyAgent(t.Context(), name))
	require.NoError(t, n.BootstrapAgent(t.Context(), name))
	require.NoError(t, n.ReadyAgent(t.Context(), name))
}
