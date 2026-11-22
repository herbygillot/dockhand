package portedit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchPlanErrorsKeepTheAssessmentReadersAnswer(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, portfile, outcome, fetchStatus, fetchDetail string
	}{
		{
			name:        "MacPorts_daemondo shape",
			portfile:    "github.setup owner fixture 1.2.3 v\ndistfiles\nlivecheck.type none",
			outcome:     OwnVersion,
			fetchStatus: Passed,
			fetchDetail: "No source is downloaded, and livecheck does not use the version; MacPorts maintains it, so update leaves it unchanged",
		},
		{
			name:        "Pothos stub shape",
			portfile:    "github.setup owner fixture 1.2.3 v\ndistfiles\nlivecheck.type none\ndepends_run-append port:fixture-child",
			outcome:     OwnVersion,
			fetchStatus: Passed,
			fetchDetail: "No source is downloaded, and livecheck does not use the version; MacPorts maintains it, so update leaves it unchanged",
		},
		{
			name:        "MacOSX.sdk shape",
			portfile:    "github.setup owner fixture 1.2.3 v\ndistfiles\nlivecheck.type regex\nlivecheck.url https://example.invalid/releases\nlivecheck.regex {fixture-(\\d+(?:\\.\\d+)*)}",
			outcome:     InputFound,
			fetchStatus: Passed,
			fetchDetail: "Fetches nothing in any observed context; an update edits its version alone",
		},
		{
			name:        "Pallet custom fetch shape",
			portfile:    "github.setup owner fixture 1.2.3 v\ndistfiles\nfetch.type svn\nsvn.url https://example.invalid/repository\nlivecheck.type none",
			outcome:     Unsupported,
			fetchStatus: Unsupported,
			fetchDetail: "portfile: unsupported source edit: custom fetching or vendored source cannot be prepared automatically here; fetch this port's sources with MacPorts instead",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, request, input := probeFixture(t, test.portfile)
			result, err := (&VersionProbe{editor: s, request: request, input: input}).Assess(t.Context(), nil)
			require.NoError(t, err)
			require.Equal(t, test.outcome, result.Outcome, "%+v", result.Findings)
			fetch := assessmentFinding(t, result, "fetch")
			require.Equal(t, test.fetchStatus, fetch.Status)
			require.Equal(t, test.fetchDetail, fetch.Detail)
		})
	}
}
