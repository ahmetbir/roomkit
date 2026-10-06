package netproto

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxNameRunes = 16
	defaultName  = "Pilot"
)

// CleanName trims a player name, drops invisible, control, zero-width,
// bidi-override and blank filler runes, keeps at most 2 combining marks per
// letter (no "zalgo" towers over the name tags), and caps it at 16 runes;
// empty becomes "Pilot".
func CleanName(s string) string {
	var b strings.Builder
	n, marks := 0, 0
	for _, r := range strings.TrimSpace(s) {
		if n == maxNameRunes {
			break
		}
		if r == utf8.RuneError || !unicode.IsGraphic(r) || hiddenRune(r) {
			continue
		}
		if unicode.In(r, unicode.Mn, unicode.Me) {
			if marks == maxMarks {
				continue
			}
			marks++
		} else {
			marks = 0
		}
		b.WriteRune(r)
		n++
	}
	if out := strings.TrimSpace(b.String()); out != "" {
		return out
	}
	return defaultName
}

const maxMarks = 2

func hiddenRune(r rune) bool {
	switch r {
	case 0x115F, 0x1160, 0x3164, 0xFFA0, 0x2800: // Hangul fillers, Braille blank
		return true
	}
	return (r >= 0x200B && r <= 0x200F) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}
