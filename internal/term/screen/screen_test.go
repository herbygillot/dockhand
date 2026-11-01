package screen

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/term"
)

// writes records each write apart.
type writes struct {
	mu   sync.Mutex
	each [][]byte
}

func (w *writes) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.each = append(w.each, append([]byte{}, p...))
	return len(p), nil
}

func (w *writes) all() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([][]byte{}, w.each...)
}

// A frame is one write: from the top left, every line the width, cut or
// padded, a reversed one reversed, and the rest of the screen erased.
func TestAFrameIsOneWriteOfLinesTheWidth(t *testing.T) {
	t.Parallel()
	in, typing := io.Pipe()
	defer typing.Close()
	out := &writes{}
	s, err := Open(term.Streams{In: in, Out: out}.WithDisplay(term.Display{Columns: 8}))
	require.NoError(t, err)
	require.Equal(t, [][]byte{[]byte(enterView)}, out.all(), "the screen is a screen of its own")

	width, height := s.Size()
	require.Equal(t, 8, width)
	require.Equal(t, defaultHeight, height, "off a terminal, a terminal's usual height")
	require.NoError(t, s.Draw([]Line{{Text: "header line, too long"}, {Text: "row", Reverse: true}, {Text: "tab\there"}}))
	drawn := out.all()
	require.Len(t, drawn, 2, "one write a frame")
	require.Equal(t, "\x1b[Hheader l\r\n\x1b[7mrow     \x1b[0m\r\ntab here\x1b[J", string(drawn[1]))

	require.NoError(t, s.Close())
	require.NoError(t, s.Close(), "closing again does nothing")
	require.Equal(t, leaveView, string(out.all()[2]), "and gives the terminal back")
}

// A frame taller than the screen is cut at its height.
func TestAFrameIsCutAtTheHeight(t *testing.T) {
	t.Parallel()
	rendered := string(Render([]Line{{Text: "a"}, {Text: "b"}, {Text: "c"}}, 2, 2))
	require.Equal(t, "\x1b[Ha \r\nb \x1b[J", rendered)
	require.Equal(t, "ab…", Fit("ab…d", 3), "cut by runes")
}

// Keys come as they're typed, and a suspended screen reads none: what's
// typed then is the command's, and after it, a key returns to the screen.
func TestASuspendedScreenReadsNoKeys(t *testing.T) {
	t.Parallel()
	in, typing := io.Pipe()
	defer typing.Close()
	var out bytes.Buffer
	var mu sync.Mutex
	locked := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return out.Write(p)
	})
	s, err := Open(term.Streams{In: in, Out: locked})
	require.NoError(t, err)
	defer s.Close()
	_, err = io.WriteString(typing, "\x1b[B")
	require.NoError(t, err)
	require.Equal(t, Key{Code: Down}, <-s.Keys())

	ran := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- s.Suspend(t.Context(), func() { close(ran) }, "Press any key to return.")
	}()
	<-ran
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.HasSuffix(out.String(), "\r\nPress any key to return.")
	}, 5*time.Second, 5*time.Millisecond)
	_, err = io.WriteString(typing, "x")
	require.NoError(t, err)
	require.NoError(t, <-done)
	mu.Lock()
	require.True(t, strings.HasSuffix(out.String(), leaveView+"\r\nPress any key to return."+enterView), "the screen is left, and entered again after the key")
	mu.Unlock()

	_, err = io.WriteString(typing, "q")
	require.NoError(t, err)
	require.Equal(t, Key{Code: Rune, Rune: 'q'}, <-s.Keys(), "the key that returned isn't read again")
	typing.Close()
	_, open := <-s.Keys()
	require.False(t, open, "the input's end ends the keys")
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
