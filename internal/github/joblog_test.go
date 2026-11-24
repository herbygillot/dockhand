package github

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJobLogRejectsNonHTTPSOrUserInfo(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://logs.example/job", "https://user@logs.example/job", "https:///job"} {
		t.Run(raw, func(t *testing.T) {
			link, err := url.Parse(raw)
			require.NoError(t, err)
			_, err = JobLog(context.Background(), &http.Client{}, link, 1024)
			require.Error(t, err)
		})
	}
}
