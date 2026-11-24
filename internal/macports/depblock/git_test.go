package depblock

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCargoBuildLockUpdateFlagsFollowEvaluatedBuildArguments(t *testing.T) {
	for _, test := range []struct {
		name    string
		options map[string]string
		flag    string
		read    bool
	}{
		{name: "PortGroup default", options: map[string]string{"cargo.offline_cmd": "--frozen --offline"}, flag: "--frozen", read: true},
		{name: "unknown arguments", options: map[string]string{}, flag: "--frozen"},
		{name: "locked also refuses lock updates", options: map[string]string{"cargo.offline_cmd": "--locked"}, flag: "--locked", read: true},
		{name: "custom build keeps frozen", options: map[string]string{"build.cmd": "/opt/local/bin/cargo build", "build.pre_args": "--release --frozen"}, flag: "--frozen", read: true},
		{name: "custom build drops frozen", options: map[string]string{"build.cmd": "/opt/local/bin/cargo build", "build.pre_args": "--release"}, read: true},
		{name: "offline alone", options: map[string]string{"cargo.offline_cmd": "--offline"}, read: true},
		{name: "explicit empty offline command", options: map[string]string{"cargo.offline_cmd": ""}, read: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			flag, read := CargoLockRefusalFlag(test.options)
			require.Equal(t, test.flag, flag)
			require.Equal(t, test.read, read)
		})
	}
}
