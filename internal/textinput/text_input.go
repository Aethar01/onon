package textinput

import (
	"unicode"
	"unicode/utf8"
)

// Input holds single-line editable text, its rune cursor, and optional selection.
type Input struct {
	Value           string
	Cursor          int
	Anchor          int
	selectionActive bool
	MouseSelecting  bool
}

func (t *Input) Reset() {
	t.Value = ""
	t.Cursor = 0
	t.Anchor = 0
	t.selectionActive = false
	t.MouseSelecting = false
}

func (t *Input) Set(value string) {
	t.Value = value
	t.Cursor = utf8.RuneCountInString(value)
	t.Anchor = t.Cursor
	t.selectionActive = false
	t.MouseSelecting = false
}

func (t *Input) InsertRune(r rune) {
	t.InsertText(string(r))
}

func (t *Input) InsertText(text string) {
	if text == "" {
		return
	}
	t.DeleteSelection()
	left, right := SplitAtRune(t.Value, t.Cursor)
	t.Value = left + text + right
	t.Cursor += utf8.RuneCountInString(text)
	t.Anchor = t.Cursor
	t.selectionActive = false
}

func (t *Input) ReplaceRange(start, end int, value string) {
	left, _ := SplitAtRune(t.Value, start)
	_, after := SplitAtRune(t.Value, end)
	t.Value = left + value + after
	t.Cursor = start + utf8.RuneCountInString(value)
	t.Anchor = t.Cursor
	t.selectionActive = false
}

func (t *Input) Backspace() {
	if t.DeleteSelection() {
		return
	}
	if t.Cursor <= 0 || t.Value == "" {
		return
	}
	left, right := SplitAtRune(t.Value, t.Cursor)
	_, size := lastRune(left)
	t.Value = left[:len(left)-size] + right
	t.Cursor--
	t.Anchor = t.Cursor
	t.selectionActive = false
}

func (t *Input) Delete() {
	if t.DeleteSelection() {
		return
	}
	runes := []rune(t.Value)
	if t.Cursor >= len(runes) {
		return
	}
	left, right := SplitAtRune(t.Value, t.Cursor)
	_, after := SplitAtRune(right, 1)
	t.Value = left + after
	t.Anchor = t.Cursor
	t.selectionActive = false
}

func (t *Input) DeleteWordLeft() {
	if t.DeleteSelection() {
		return
	}
	if t.Cursor <= 0 || t.Value == "" {
		return
	}
	runes := []rune(t.Value)
	end := clampInt(t.Cursor, 0, len(runes))
	start := wordLeftPosition(runes, end)
	t.Value = string(runes[:start]) + string(runes[end:])
	t.Cursor = start
	t.Anchor = start
	t.selectionActive = false
}

// DeleteWordRight deletes to the end of the word after the cursor.
func (t *Input) DeleteWordRight() {
	if t.DeleteSelection() {
		return
	}
	runes := []rune(t.Value)
	start := clampInt(t.Cursor, 0, len(runes))
	t.deleteRange(start, wordRightPosition(runes, start))
}

// DeleteToStart deletes from the start of the input to the cursor.
func (t *Input) DeleteToStart() {
	if t.DeleteSelection() {
		return
	}
	t.deleteRange(0, t.Cursor)
}

// DeleteToEnd deletes from the cursor to the end of the input.
func (t *Input) DeleteToEnd() {
	if t.DeleteSelection() {
		return
	}
	t.deleteRange(t.Cursor, utf8.RuneCountInString(t.Value))
}

func (t *Input) deleteRange(start, end int) {
	runes := []rune(t.Value)
	start = clampInt(start, 0, len(runes))
	end = clampInt(end, start, len(runes))
	t.Value = string(runes[:start]) + string(runes[end:])
	t.Cursor = start
	t.Anchor = start
	t.selectionActive = false
}

// SelectAll selects the whole input, leaving the cursor at its end.
func (t *Input) SelectAll() {
	t.SetCursor(0, false)
	t.MoveToEnd(true)
}

// MoveToStart moves the cursor to the start of the input, selecting the
// text it passes when extend is set.
func (t *Input) MoveToStart(extend bool) {
	t.SetCursor(0, extend)
}

// MoveToEnd moves the cursor to the end of the input, selecting the text
// it passes when extend is set.
func (t *Input) MoveToEnd(extend bool) {
	t.SetCursor(utf8.RuneCountInString(t.Value), extend)
}

func (t *Input) Move(delta int) {
	t.MoveSelecting(delta, false)
}

func (t *Input) MoveSelecting(delta int, extend bool) {
	length := utf8.RuneCountInString(t.Value)
	if !extend && t.HasSelection() {
		start, end, _ := t.SelectionRange()
		if delta < 0 {
			t.SetCursor(start, false)
		} else if delta > 0 {
			t.SetCursor(end, false)
		}
		return
	}
	t.SetCursor(clampInt(t.Cursor+delta, 0, length), extend)
}

func (t *Input) MoveWordLeft() {
	t.MoveWordLeftSelecting(false)
}

func (t *Input) MoveWordLeftSelecting(extend bool) {
	if !extend && t.HasSelection() {
		start, _, _ := t.SelectionRange()
		t.SetCursor(start, false)
		return
	}
	if t.Cursor <= 0 || t.Value == "" {
		return
	}
	runes := []rune(t.Value)
	pos := wordLeftPosition(runes, clampInt(t.Cursor, 0, len(runes)))
	t.SetCursor(pos, extend)
}

func (t *Input) MoveWordRight() {
	t.MoveWordRightSelecting(false)
}

func (t *Input) MoveWordRightSelecting(extend bool) {
	if !extend && t.HasSelection() {
		_, end, _ := t.SelectionRange()
		t.SetCursor(end, false)
		return
	}
	runes := []rune(t.Value)
	if t.Cursor >= len(runes) {
		return
	}
	pos := wordRightPosition(runes, clampInt(t.Cursor, 0, len(runes)))
	t.SetCursor(pos, extend)
}

func (t *Input) SetCursor(pos int, extend bool) {
	pos = clampInt(pos, 0, utf8.RuneCountInString(t.Value))
	if extend {
		if !t.selectionActive {
			t.Anchor = t.Cursor
		}
		t.Cursor = pos
		t.selectionActive = t.Cursor != t.Anchor
		return
	}
	t.Cursor = pos
	t.Anchor = pos
	t.selectionActive = false
}

func (t Input) HasSelection() bool {
	return t.selectionActive && t.Cursor != t.Anchor
}

func (t Input) SelectionRange() (int, int, bool) {
	if !t.HasSelection() {
		return 0, 0, false
	}
	start, end := t.Anchor, t.Cursor
	if start > end {
		start, end = end, start
	}
	length := utf8.RuneCountInString(t.Value)
	start = clampInt(start, 0, length)
	end = clampInt(end, 0, length)
	return start, end, start != end
}

func (t Input) SelectedText() (string, bool) {
	start, end, ok := t.SelectionRange()
	if !ok {
		return "", false
	}
	_, rest := SplitAtRune(t.Value, start)
	selected, _ := SplitAtRune(rest, end-start)
	return selected, true
}

func (t *Input) DeleteSelection() bool {
	start, end, ok := t.SelectionRange()
	if !ok {
		return false
	}
	left, _ := SplitAtRune(t.Value, start)
	_, right := SplitAtRune(t.Value, end)
	t.Value = left + right
	t.Cursor = start
	t.Anchor = start
	t.selectionActive = false
	return true
}

func (t Input) Left() string {
	left, _ := SplitAtRune(t.Value, t.Cursor)
	return left
}

// SplitAtRune splits s into the prefix of pos runes and the remainder.
func SplitAtRune(s string, pos int) (string, string) {
	if pos <= 0 {
		return "", s
	}
	runes := []rune(s)
	if pos >= len(runes) {
		return s, ""
	}
	return string(runes[:pos]), string(runes[pos:])
}

func lastRune(s string) (rune, int) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i]&0xc0 != 0x80 {
			return []rune(s[i:])[0], len(s) - i
		}
	}
	return 0, 0
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func wordLeftPosition(runes []rune, pos int) int {
	for pos > 0 && unicode.IsSpace(runes[pos-1]) {
		pos--
	}
	for pos > 0 && !unicode.IsSpace(runes[pos-1]) {
		pos--
	}
	return pos
}

func wordRightPosition(runes []rune, pos int) int {
	for pos < len(runes) && unicode.IsSpace(runes[pos]) {
		pos++
	}
	for pos < len(runes) && !unicode.IsSpace(runes[pos]) {
		pos++
	}
	return pos
}
