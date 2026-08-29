package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupProject(t *testing.T) {
	url, err := GitHub.LookupProject("openai", "tart")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/openai/tart", url)

	url, err = SourceHut.LookupProject("sircmpwn", "aerc")
	require.NoError(t, err)
	assert.Equal(t, "https://git.sr.ht/~sircmpwn/aerc", url)

	url, err = Cgit.At("git.zx2c4.com").LookupProject("", "wireguard-tools")
	require.NoError(t, err)
	assert.Equal(t, "https://git.zx2c4.com/wireguard-tools.git", url)

	// Self-hosted families are unbound until At.
	_, err = Gitea.LookupProject("a", "p")
	require.ErrorIs(t, err, ErrUnbound)

	// Author is required except where the forge has none.
	_, err = GitHub.LookupProject("", "p")
	require.Error(t, err)
}

func TestAt(t *testing.T) {
	// Scheme normalized away; paths kept (gitea under a subpath).
	assert.Equal(t, "gitlab.com", GitLab.At("https://gitlab.com").Domain)
	assert.Equal(t, "example.org/gitea", Gitea.At("example.org/gitea/").Domain)
	// The canonical var is untouched.
	assert.Equal(t, "gitlab.com", GitLab.Domain)
	assert.Empty(t, Gitea.Domain)
}

func TestFromRepoURL(t *testing.T) {
	f, ok := FromRepoURL("https://github.com/openai/tart")
	require.True(t, ok)
	assert.Same(t, GitHub, f)

	f, ok = FromRepoURL("https://codeberg.org/dnkl/foot")
	require.True(t, ok)
	assert.Same(t, Codeberg, f)

	_, ok = FromRepoURL("https://git.example.org/a/p")
	assert.False(t, ok)
	_, ok = FromRepoURL("not a url")
	assert.False(t, ok)
}

func TestAllCoversKnownForges(t *testing.T) {
	assert.Len(t, All, 8)
	assert.Contains(t, All, GitHub)
	assert.NotContains(t, All, None)
}

func TestUnidentifiableIsNone(t *testing.T) {
	f, ok := FromRepoURL("https://git.example.org/a/p")
	assert.False(t, ok)
	assert.Same(t, None, f)

	// None still builds plain git URLs once bound.
	url, err := None.At("git.example.org").LookupProject("a", "p")
	require.NoError(t, err)
	assert.Equal(t, "https://git.example.org/a/p", url)
}
