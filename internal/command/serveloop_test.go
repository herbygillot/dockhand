package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

func TestServeWorksThroughYourOutdatedPorts(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	withScript(t, w, "passed")
	withOutdated(t)
	poll := servePoll
	t.Cleanup(func() { servePoll = poll })
	servePoll = 20 * time.Millisecond
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	now := serveNow
	t.Cleanup(func() { serveNow = now })
	morning := time.Date(2026, 9, 25, 8, 0, 0, 0, time.Local)
	serveNow = func() time.Time { return morning }
	var mu sync.Mutex
	var notes []string
	post := postNotification
	t.Cleanup(func() { postNotification = post })
	postNotification = func(title, text string) error {
		mu.Lock()
		defer mu.Unlock()
		notes = append(notes, title+": "+text)
		return nil
	}

	config := filepath.Join(w.home, ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"{@ada example.org:ada}\"\n"), append(data, []byte("\n[serve]\nfor_outdated = \"check\"\n")...)...), 0o644))

	serveUntil := func(args []string, until ...string) string {
		ctx, stop := context.WithCancel(t.Context())
		var served syncBuffer
		done := make(chan error)
		go func() {
			done <- Run(ctx, append([]string{"serve"}, args...), Streams{In: strings.NewReader(""), Out: &served, Err: &served})
		}()
		for _, text := range until {
			require.Eventually(t, func() bool { return strings.Contains(served.String(), text) }, 10*time.Second, 10*time.Millisecond, served.String())
		}
		stop()
		require.NoError(t, <-done)
		return served.String()
	}

	out := serveUntil([]string{"--submit-passing"}, "serve: opened #34901 for jq-")
	require.Contains(t, out, "opens PRs for passing updates it prepared, at most 10 a day\n")
	require.Contains(t, out, "serve: 1 port of yours has newer releases: jq\n")
	require.Regexp(t, `serve: prepared jq-[a-z0-9]{4}: 1\.7\.1 → 1\.8\.1, check-1 queued\n`, out)
	require.Regexp(t, `check-1 jq-[a-z0-9]{4}: passed\n`, out)
	require.Contains(t, g.prs[0].Body, engine.ServeNote)
	mu.Lock()
	require.GreaterOrEqual(t, len(notes), 3, "prepared, passed, and opened")
	mu.Unlock()

	t.Setenv("MACPORTS_TREE", w.clone)
	status, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Regexp(t, `· jq-[a-z0-9]{4}\s+#34901 opened by serve, without a person's review`, status)
	require.Contains(t, status, "Your ports: 1 port has newer releases, as serve found ")

	again := serveUntil(nil, "serve: leading")
	require.NotContains(t, again, "newer releases", "once a day")
	require.Contains(t, again, "opens no pull requests; it only checks", "--submit-passing was for one run")
}

func TestServeSubmitsNoMoreThanTheDailyLimit(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	withScript(t, w, "passed")
	withOutdated(t)
	poll := servePoll
	t.Cleanup(func() { servePoll = poll })
	servePoll = 20 * time.Millisecond
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	now := serveNow
	t.Cleanup(func() { serveNow = now })
	morning := time.Date(2026, 9, 25, 8, 0, 0, 0, time.Local)
	serveNow = func() time.Time { return morning }
	config := filepath.Join(w.home, ".dockhand", "config.toml")
	data, err := os.ReadFile(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config, append([]byte("maintainer = \"@ada\"\n"), append(data, []byte("\n[serve]\nfor_outdated = \"check\"\nsubmit_passing = true\nsubmit_limit = 1\nnotify = false\n")...)...), 0o644))
	// One pull request was opened earlier today, by an earlier serve, and
	// one yesterday, which today's limit doesn't count.
	e, err := (&settings{}).open(t.Context())
	require.NoError(t, err)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		for _, at := range []time.Time{morning.Add(-25 * time.Hour), morning.Add(-time.Hour)} {
			if _, err := tx.AppendEvent(model.Event{At: at, Kind: engine.ServeSubmitKind, Message: "serve opened a pull request"}); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, e.Close())

	ctx, stop := context.WithCancel(t.Context())
	var served syncBuffer
	done := make(chan error)
	go func() {
		done <- Run(ctx, []string{"serve"}, Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool {
		return strings.Contains(served.String(), "waits for tomorrow; today's limit of 1 pull request is reached")
	}, 10*time.Second, 10*time.Millisecond, served.String())
	stop()
	require.NoError(t, <-done)
	require.Empty(t, g.prs, "the limit holds across restarts")
	require.Contains(t, served.String(), "at most 1 a day", "the config's submit_passing turns it on")
}
