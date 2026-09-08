package progress

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder is what a caller would implement, and writing one here is the
// point: a Sink is three methods and no state, so a test double for the
// whole of narration is nine lines.
type recorder struct {
	stages []string
	said   []string
	read   int
}

func (r *recorder) Stage(op, stage string) { r.stages = append(r.stages, op+"/"+stage) }
func (r *recorder) Say(l Level, s string)  { r.said = append(r.said, s) }
func (r *recorder) Stream(rd io.Reader) {
	b, _ := io.ReadAll(rd)
	r.read += len(b)
}

// NOTHING A CALLER NEEDS TRAVELS THROUGH A SINK. The methods return
// nothing, so this is a compile-time claim as much as a test: the
// assignment below does not compile if a method ever grows a return.
func TestASinkAnswersNothing(t *testing.T) {
	var s Sink = &recorder{}
	s.Stage("bump", "mint")
	s.Say(Warn, "unverified")
	s.Stream(strings.NewReader("build log"))

	r := s.(*recorder)
	assert.Equal(t, []string{"bump/mint"}, r.stages)
	assert.Equal(t, []string{"unverified"}, r.said)
	assert.Equal(t, 9, r.read)
}

// DISCARD IS THE ZERO SINK, and it keeps nothing — including the reader
// it is handed, which it does not drain. A JSON caller and a test wire
// this and never think about it again.
func TestDiscardKeepsNothing(t *testing.T) {
	var s Sink = Discard{}
	require.NotPanics(t, func() {
		s.Stage("cycle", "drain")
		s.Say(Info, "")
		s.Stream(strings.NewReader("ignored"))
	})
}

// THE ZERO LEVEL IS Info, which is the ordinary line. There is nothing
// here for rule 7 to protect: a narration carries no fact anybody
// decides from, so its zero cannot mean "I could not find out".
func TestZeroLevelIsInfo(t *testing.T) {
	var l Level
	assert.Equal(t, Info, l)
	assert.NotEqual(t, Info, Warn)
	assert.NotEqual(t, Warn, Detail)
}
