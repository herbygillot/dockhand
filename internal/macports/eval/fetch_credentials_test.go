package eval

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchCredentialsUseInstalledSelectorWithoutExposingSecrets(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, configured, sites, files, extra, want string }{
		{"matching host", "example.invalid secret-value", "https://example.invalid/archive", "source.tar.gz", "", "1"},
		{"unrelated host", "other.invalid secret-value", "https://example.invalid/archive", "source.tar.gz", "", "0"},
		{"different scheme and port", "example.invalid secret-value", "http://example.invalid:8080/archive", "source.tar.gz", "", "1"},
		{"full URL is not a host key", "https://example.invalid/archive secret-value", "https://example.invalid/archive", "source.tar.gz", "", "0"},
		{"unused tagged site", "private.invalid secret-value", "https://example.invalid/archive:source https://private.invalid/unused:other", "source.tar.gz:source", "", "0"},
		{"selected auxiliary archive", "private.invalid secret-value", "https://example.invalid/archive:source https://private.invalid/assets:other", "source.tar.gz:source asset.tar.gz:other", "", "1"},
		{"per-port credentials", "", "https://example.invalid/archive", "source.tar.gz", "fetch.user private-user\nfetch.password secret-value\n", "1"},
		{"empty matching override", "example.invalid {}", "https://example.invalid/archive", "source.tar.gz", "fetch.password secret-value\n", "0"},
		{"unrelated override preserves per-port credentials", "other.invalid secret-value", "https://example.invalid/archive", "source.tar.gz", "fetch.user private-user\n", "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			evaluator := liveEvaluator(t)
			tree := fixtureTree(t)
			putFile(t, tree.Root(), "devel/fixture/Portfile", "PortSystem 1.0\nname fixture\nversion 1\ncategories devel\nmaster_sites "+test.sites+"\ndistfiles "+test.files+"\n"+test.extra)
			session, _, err := evaluator.start(t.Context(), tree)
			require.NoError(t, err)
			defer session.Close()
			supported, err := session.Call(t.Context(), "eval", "expr {[llength [info procs ::macports::_curlwrap_credential_args]] != 0}")
			require.NoError(t, err)
			if supported != "1" {
				t.Skip("host-matching selector requires current MacPorts")
			}
			_, err = session.Call(t.Context(), "eval", "set ::macports::fetch_credentials {"+test.configured+"}")
			require.NoError(t, err)
			reply, err := session.Call(t.Context(), "metadata", filepath.Join(tree.Root(), "devel/fixture"), "")
			require.NoError(t, err)
			require.NotContains(t, reply, "secret-value")
			require.NotContains(t, reply, "private-user")
			info, _, err := decodeMetadata(reply)
			require.NoError(t, err)
			require.Empty(t, info.OptionErrors["fetch.has_credentials"])
			require.Equal(t, test.want, info.Options["fetch.has_credentials"])
			for _, key := range []string{"fetch.user", "fetch.password", "fetch_credentials", "macports::fetch_credentials"} {
				require.NotContains(t, info.Options, key)
			}
		})
	}
}

func TestFetchCredentialsFollowLegacySiteMatchingAndFailClosed(t *testing.T) {
	t.Parallel()
	const legacy = `proc ::macports::curlwrap {action site fallback args} {
    variable fetch_credentials
    if {[dict exists $fetch_credentials $site]} {set fallback [dict get $fetch_credentials $site]}
    if {$fallback eq ""} {return [curl $action {*}$args]}
    return [curl $action -u $fallback {*}$args]
 }`
	for _, test := range []struct {
		name, selector, configured, extra, want string
		failed                                  bool
	}{
		{"legacy exact site", legacy, "https://example.invalid/archive secret-value", "", "1", false},
		{"legacy does not match host", legacy, "example.invalid secret-value", "", "0", false},
		{"legacy preserves trailing slash distinction", legacy, "https://example.invalid/archive/ secret-value", "", "0", false},
		{"legacy empty override", legacy, "https://example.invalid/archive {}", "fetch.password secret-value\n", "0", false},
		{"unknown implementation cannot open network", `proc ::macports::curlwrap {args} {socket example.invalid 443}`, "example.invalid secret-value", "", "1", true},
		{"selector does not call curl", `proc ::macports::curlwrap {args} {return 0}`, "example.invalid secret-value", "", "1", true},
		{"selector error is redacted", `proc ::macports::curlwrap {args} {error secret-value}`, "example.invalid secret-value", "", "1", true},
		{"option error is redacted", legacy, "", "default fetch.password {[error secret-value]}\n", "1", true},
		{"malformed configuration", legacy, "example.invalid", "", "1", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			evaluator := liveEvaluator(t)
			tree := fixtureTree(t)
			putFile(t, tree.Root(), "devel/fixture/Portfile", "PortSystem 1.0\nname fixture\nversion 1\ncategories devel\nmaster_sites https://example.invalid/archive:source\ndistfiles source.tar.gz:source\n"+test.extra)
			session, _, err := evaluator.start(t.Context(), tree)
			require.NoError(t, err)
			defer session.Close()
			_, err = session.Call(t.Context(), "eval", test.selector+"\nset ::macports::fetch_credentials {"+test.configured+"}")
			require.NoError(t, err)
			reply, err := session.Call(t.Context(), "metadata", filepath.Join(tree.Root(), "devel/fixture"), "")
			require.NoError(t, err)
			require.NotContains(t, reply, "secret-value")
			info, _, err := decodeMetadata(reply)
			require.NoError(t, err)
			require.Equal(t, test.want, info.Options["fetch.has_credentials"])
			if test.failed {
				require.Equal(t, "cannot determine applicable fetch credentials", info.OptionErrors["fetch.has_credentials"])
			} else {
				require.Empty(t, info.OptionErrors["fetch.has_credentials"])
			}
		})
	}
}
