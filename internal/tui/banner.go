package tui

import (
	"strings"
	"unicode"
)

// A tiny pixel font, 4 pixels tall, drawn two pixels per cell with half
// blocks, so each letter is 2 lines. Artist pages spell names with it.
var bannerFont = map[rune][4]string{
	'A':  {".X.", "X.X", "XXX", "X.X"},
	'B':  {"XX.", "XXX", "X.X", "XXX"},
	'C':  {".XX", "X..", "X..", ".XX"},
	'D':  {"XX.", "X.X", "X.X", "XX."},
	'E':  {"XXX", "XX.", "X..", "XXX"},
	'F':  {"XXX", "XX.", "X..", "X.."},
	'G':  {".XX", "X..", "X.X", ".XX"},
	'H':  {"X.X", "XXX", "X.X", "X.X"},
	'I':  {"XXX", ".X.", ".X.", "XXX"},
	'J':  {"..X", "..X", "X.X", ".X."},
	'K':  {"X.X", "XX.", "X.X", "X.X"},
	'L':  {"X..", "X..", "X..", "XXX"},
	'M':  {"X...X", "XX.XX", "X.X.X", "X...X"},
	'N':  {"X..X", "XX.X", "X.XX", "X..X"},
	'O':  {".X.", "X.X", "X.X", ".X."},
	'P':  {"XX.", "X.X", "XX.", "X.."},
	'Q':  {".X.", "X.X", "X.X", ".XX"},
	'R':  {"XX.", "X.X", "XX.", "X.X"},
	'S':  {".XX", "XX.", "..X", "XX."},
	'T':  {"XXX", ".X.", ".X.", ".X."},
	'U':  {"X.X", "X.X", "X.X", "XXX"},
	'V':  {"X.X", "X.X", "X.X", ".X."},
	'W':  {"X...X", "X...X", "X.X.X", ".X.X."},
	'X':  {"X.X", ".X.", ".X.", "X.X"},
	'Y':  {"X.X", "X.X", ".X.", ".X."},
	'Z':  {"XXX", "..X", "X..", "XXX"},
	'0':  {"XXX", "X.X", "X.X", "XXX"},
	'1':  {".X.", "XX.", ".X.", "XXX"},
	'2':  {"XX.", "..X", ".X.", "XXX"},
	'3':  {"XX.", ".X.", "..X", "XX."},
	'4':  {"X.X", "X.X", "XXX", "..X"},
	'5':  {"XXX", "XX.", "..X", "XX."},
	'6':  {"X..", "XXX", "X.X", "XXX"},
	'7':  {"XXX", "..X", ".X.", ".X."},
	'8':  {"XXX", "XXX", "X.X", "XXX"},
	'9':  {"XXX", "X.X", "XXX", "..X"},
	' ':  {"..", "..", "..", ".."},
	'-':  {"...", "XXX", "...", "..."},
	'.':  {".", ".", ".", "X"},
	'!':  {"X", "X", ".", "X"},
	'?':  {"XX.", "..X", ".X.", ".X."},
	'&':  {".X.", "X.X", ".X.", "X.X"},
	'\'': {"X", "X", ".", "."},
	'$':  {".XX", "XX.", "..X", "XX."},
	'+':  {"...", ".X.", "XXX", ".X."},
}

// banner spells s in the pixel font, as 2 lines, or reports false when a
// letter isn't in the font.
func banner(s string) ([2]string, bool) {
	var rows [2]strings.Builder
	for i, r := range []rune(strings.ToUpper(s)) {
		glyph, ok := bannerFont[unicode.ToUpper(r)]
		if !ok {
			return [2]string{}, false
		}
		if i > 0 {
			rows[0].WriteByte(' ')
			rows[1].WriteByte(' ')
		}
		for line := range 2 {
			top, bottom := glyph[2*line], glyph[2*line+1]
			for x := range len(top) {
				rows[line].WriteString(halfBlock(top[x] == 'X', bottom[x] == 'X'))
			}
		}
	}
	return [2]string{rows[0].String(), rows[1].String()}, true
}

func halfBlock(top, bottom bool) string {
	switch {
	case top && bottom:
		return "█"
	case top:
		return "▀"
	case bottom:
		return "▄"
	}
	return " "
}
