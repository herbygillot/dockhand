package text

type Span struct {
	Start int
	End   int
}

func (s Span) Bytes(src []byte) []byte { return src[s.Start:s.End] }

func (s Span) Text(src []byte) string { return string(src[s.Start:s.End]) }

func (s Span) Len() int { return s.End - s.Start }

func Position(src []byte, offset int) (line, col int) {
	line, col = 1, 1
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
