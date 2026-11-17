package macports

import "unicode/utf8"

// HostCallText bounds a human-facing call fragment. Evidence keeps the full
// call; shortening it before matching can conflate different questions.
func HostCallText(text string) string {
	const shown = 200
	if utf8.RuneCountInString(text) <= shown {
		return text
	}
	return string([]rune(text)[:shown]) + "..."
}
