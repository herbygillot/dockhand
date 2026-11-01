package screen

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

// Code is which key a Key is: a rune typed, or one of the keys that type
// none.
type Code int

const (
	// Rune is a character typed, which Key.Rune holds.
	Rune Code = iota
	Up
	Down
	PageUp
	PageDown
	Home
	End
	Enter
	Escape
	// Interrupt is Ctrl-C, which raw mode reads as a key rather than a
	// signal.
	Interrupt
)

// Key is one key pressed.
type Key struct {
	Code Code
	Rune rune
}

const escape = 0x1b

// sequences are the escape sequences a terminal sends for the keys a view
// reads, each after its ESC: xterm's and VT220's, and the application
// mode's ESC O forms.
var sequences = map[string]Code{
	"[A": Up, "[B": Down, "OA": Up, "OB": Down,
	"[5~": PageUp, "[6~": PageDown,
	"[H": Home, "[1~": Home, "[7~": Home, "OH": Home,
	"[F": End, "[4~": End, "[8~": End, "OF": End,
}

// decoder turns the bytes a terminal sends into keys, holding a sequence
// one read split for the next.
type decoder struct {
	pending []byte
}

// feed is the keys p completes, with what came before it; a sequence not
// yet ended is held, and one ended that names no key here is dropped, as
// is a control character other than Enter's and Ctrl-C's.
func (d *decoder) feed(p []byte) []Key {
	data := append(d.pending, p...)
	d.pending = nil
	var keys []Key
	for len(data) > 0 {
		switch b := data[0]; {
		case b == escape:
			size, key, known, complete := sequence(data)
			if !complete {
				d.pending = append([]byte{}, data...)
				return keys
			}
			if known {
				keys = append(keys, key)
			}
			data = data[size:]
		case b == '\r' || b == '\n':
			keys = append(keys, Key{Code: Enter})
			data = data[1:]
		case b == 0x03:
			keys = append(keys, Key{Code: Interrupt})
			data = data[1:]
		case b < 0x20 || b == 0x7f:
			data = data[1:]
		default:
			if !utf8.FullRune(data) {
				d.pending = append([]byte{}, data...)
				return keys
			}
			r, size := utf8.DecodeRune(data)
			if r != utf8.RuneError && unicode.IsPrint(r) {
				keys = append(keys, Key{Code: Rune, Rune: r})
			}
			data = data[size:]
		}
	}
	return keys
}

// flush is what's held once nothing more has come: an ESC alone is the
// Escape key, and any other sequence begun is dropped.
func (d *decoder) flush() []Key {
	held := d.pending
	d.pending = nil
	if len(held) == 1 && held[0] == escape {
		return []Key{{Code: Escape}}
	}
	return nil
}

// sequence reads the escape sequence data begins with: how many bytes it
// takes, its key where it names one here, and whether it's ended yet. A
// CSI sequence ends at its final byte, ESC O at the byte after the O; ESC
// before anything else is the Escape key, and the byte after it is read
// on its own.
func sequence(data []byte) (size int, key Key, known, complete bool) {
	if len(data) < 2 {
		return 0, Key{}, false, false
	}
	switch data[1] {
	case 'O':
		if len(data) < 3 {
			return 0, Key{}, false, false
		}
		code, ok := sequences[string(data[1:3])]
		return 3, Key{Code: code}, ok, true
	case '[':
		// Parameters and intermediates, then the final byte, as ECMA-48
		// sets them.
		end := bytes.IndexFunc(data[2:], func(r rune) bool { return r >= 0x40 && r <= 0x7e })
		if end < 0 {
			return 0, Key{}, false, false
		}
		size = end + 3
		code, ok := sequences[string(data[1:size])]
		return size, Key{Code: code}, ok, true
	}
	return 1, Key{Code: Escape}, true, true
}
