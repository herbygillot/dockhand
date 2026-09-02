package verify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The marker's bytes are a contract between a runner and the splitter,
// so they are pinned here rather than derived from the constant the
// splitter also reads.
func TestSubjectMarkerIsOneLineNamingThePort(t *testing.T) {
	assert.Equal(t, "===> dockhand subject: jq", SubjectMarker("jq"))
	assert.NotContains(t, SubjectMarker("jq"), "\n", "the newline is the runner's to add")
}

func TestSplitSubjectsTable(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want map[string]string
	}{
		{"an empty log has no subjects", "", map[string]string{}},

		// Today's shape: nothing prints a marker, so the whole log is
		// the one implicit subject.
		{"a log with no marker is the implicit subject",
			"--->  Building jq\nError: Failed to build jq\n",
			map[string]string{"": "--->  Building jq\nError: Failed to build jq\n"}},
		{"a final line without a newline is kept",
			"Error: boom",
			map[string]string{"": "Error: boom"}},

		{"one marker names what follows it",
			"===> dockhand subject: jq\n--->  Building jq\n",
			map[string]string{"jq": "--->  Building jq\n"}},
		{"text before the first marker stays implicit",
			"--->  Staging portdirs\n===> dockhand subject: jq\n--->  Building jq\n",
			map[string]string{
				"":   "--->  Staging portdirs\n",
				"jq": "--->  Building jq\n",
			}},
		{"several markers each take the lines beneath them",
			"===> dockhand subject: jq\n--->  Building jq\n" +
				"===> dockhand subject: curl\n--->  Building curl\nError: Failed to build curl\n" +
				"===> dockhand subject: oniguruma\n--->  Building oniguruma\n",
			map[string]string{
				"jq":        "--->  Building jq\n",
				"curl":      "--->  Building curl\nError: Failed to build curl\n",
				"oniguruma": "--->  Building oniguruma\n",
			}},

		// A port that said nothing is a different fact from a port that
		// was never in the cohort, so the announced subject survives.
		{"an announced subject with no output is still present",
			"===> dockhand subject: jq\n===> dockhand subject: curl\n--->  Building curl\n",
			map[string]string{"jq": "", "curl": "--->  Building curl\n"}},
		{"a subject named twice appends rather than replaces",
			"===> dockhand subject: jq\n--->  Building jq\n" +
				"===> dockhand subject: curl\n--->  Building curl\n" +
				"===> dockhand subject: jq\n--->  Testing jq\n",
			map[string]string{
				"jq":   "--->  Building jq\n--->  Testing jq\n",
				"curl": "--->  Building curl\n",
			}},

		// Noise around a marker line: the line is trimmed, so a log that
		// crossed a terminal still splits.
		{"an indented marker still splits",
			"    ===> dockhand subject: jq\n--->  Building jq\n",
			map[string]string{"jq": "--->  Building jq\n"}},
		{"a CRLF marker still splits",
			"===> dockhand subject: jq\r\n--->  Building jq\n",
			map[string]string{"jq": "--->  Building jq\n"}},
		{"trailing spaces are not part of the name",
			"===> dockhand subject: jq   \n--->  Building jq\n",
			map[string]string{"jq": "--->  Building jq\n"}},

		// Noise on the marker line: it is not a marker, and splits
		// nothing. A build that echoes the runner's command must not
		// hand one port's output to another.
		{"a marker quoted mid-line is not one",
			"DEBUG: echo ===> dockhand subject: jq\n--->  Building curl\n",
			map[string]string{"": "DEBUG: echo ===> dockhand subject: jq\n--->  Building curl\n"}},
		{"a marker with a prefix on its line is not one",
			"DEBUG: ===> dockhand subject: jq\n",
			map[string]string{"": "DEBUG: ===> dockhand subject: jq\n"}},
		{"the prefix wants its space",
			"===> dockhand subject:jq\n",
			map[string]string{"": "===> dockhand subject:jq\n"}},
		{"a nameless marker names nothing and is ordinary text",
			"===> dockhand subject: \n--->  Building jq\n",
			map[string]string{"": "===> dockhand subject: \n--->  Building jq\n"}},
		{"a lookalike with MacPorts' own arrow is not a marker",
			"--->  dockhand subject: jq\n",
			map[string]string{"": "--->  dockhand subject: jq\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SplitSubjects(tc.log))
		})
	}
}

// Every log today is markerless, and the readers that summarize a
// failure run over the bytes this hands back. It must be the log
// itself, not a reconstruction of it.
func TestSplitSubjectsReturnsAMarkerlessLogByteForByte(t *testing.T) {
	log := "--->  Verifying Portfile for jq\n" +
		"--->  0 errors and 0 warnings found.\r\n" +
		"DEBUG: Found port in file:///tmp/dockhand-overlay/sysutils/jq\n" +
		"\n" +
		"Error: Failed to build jq: command execution failed\n" +
		"Error: See /opt/local/var/macports/logs/x/main.log for details."
	subjects := SplitSubjects(log)
	assert.Len(t, subjects, 1, "a markerless log is one subject")
	assert.Equal(t, log, subjects[""], "the implicit subject is the whole log, unchanged")
}

// The accessor is what a judgment reads, and the empty key is the trap
// it exists to close: today's logs carry no marker, so a caller that
// indexed the map by port name would summarize an empty log for every
// real failure there is.
func TestSubjectLogTable(t *testing.T) {
	cohort := "--->  Staging portdirs\n" +
		"===> dockhand subject: jq\n--->  Building jq\n" +
		"===> dockhand subject: curl\nError: Failed to build curl\n"
	cases := []struct {
		name, log, port, want string
	}{
		{"a markerless log is the port's log whatever it is called",
			"Error: Failed to build jq\n", "jq", "Error: Failed to build jq\n"},
		{"and for a port the log never names, because nothing claimed it",
			"Error: Failed to build jq\n", "curl", "Error: Failed to build jq\n"},
		{"an empty log stays empty", "", "jq", ""},
		{"a marked subject is its own section", cohort, "jq", "--->  Building jq\n"},
		{"and the neighbour's is the neighbour's", cohort, "curl", "Error: Failed to build curl\n"},
		// The prologue belongs to the runner, not to a port that was
		// never in the cohort.
		{"a port the cohort did not run gets nothing", cohort, "oniguruma", ""},
		{"a subject that said nothing said nothing",
			"===> dockhand subject: jq\n", "jq", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SubjectLog(tc.log, tc.port))
		})
	}
}

// The runner writes markers with SubjectMarker and the splitter reads
// them back; a change to the prefix that broke the pair would pass every
// literal above and fail here.
func TestSplitSubjectsReadsBackWhatSubjectMarkerWrites(t *testing.T) {
	ports := []string{"jq", "py313-numpy", "gtk3-devel", "R"}
	var b strings.Builder
	for _, p := range ports {
		b.WriteString(SubjectMarker(p) + "\n")
		b.WriteString("--->  Building " + p + "\n")
	}
	subjects := SplitSubjects(b.String())
	assert.Len(t, subjects, len(ports))
	for _, p := range ports {
		assert.Equal(t, "--->  Building "+p+"\n", subjects[p])
	}
}
