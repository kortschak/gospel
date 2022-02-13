// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"unicode"
	"unicode/utf8"
)

// words provides a word scanner for bufio.Scanner that can report the
// position of the last found word in the scanner source.
type words struct {
	current span

	doubleQuoted bool
}

type span struct {
	pos, end int
}

// ScanWords is derived from the bufio.ScanWords split functions.
//
// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// ScanWords is a split function for a Scanner that returns each
// space/punctuation-separated word of text, with surrounding spaces
// deleted. It will never return an empty string. The definition of
// space/punctuation is set by unicode.IsSpace and unicode.IsPunct.
func (w *words) ScanWords(data []byte, atEOF bool) (advance int, token []byte, err error) {
	start := 0
	w.current.pos = w.current.end
	var prev rune
	for width, i := 0, 0; start < len(data); start += width {
		var r rune
		r, width = utf8.DecodeRune(data[start:])
		if !isSplitter(prev, r, data[i+width:]) {
			prev = r
			break
		}
		wid, ok := isEscapeSplitter(r, data[i+width:], w.doubleQuoted)
		width += wid
		if !ok {
			prev = r
			break
		}
		prev = r
	}
	w.current.pos += start

	// Scan until space, marking end of word.
	for width, i := 0, start; i < len(data); i += width {
		var r rune
		r, width = utf8.DecodeRune(data[i:])
		if isSplitter(prev, r, data[i+width:]) {
			w.current.end += i + width
			return i + width, data[start:i], nil
		}
		wid, ok := isEscapeSplitter(r, data[i+width:], w.doubleQuoted)
		width += wid
		if ok {
			w.current.end += i + width
			return i + width, data[start:i], nil
		}
		prev = r
	}
	// If we're at EOF, we have a final, non-empty, non-terminated word. Return it.
	if atEOF && len(data) > start {
		w.current.end += len(data)
		return len(data), data[start:], nil
	}
	// Request more data.
	w.current.end = w.current.pos
	return start, nil, nil
}

func isSplitter(prev, curr rune, next []byte) bool {
	return unicode.IsSpace(curr) || unicode.IsSymbol(curr) || isWordSplitPunct(prev, curr, next)
}

// isWordSplitPunct returns whether the previous, current and next runes
// indicate that the current rune splits words.
func isWordSplitPunct(prev, curr rune, next []byte) bool {
	return curr != '_' && curr != '\\' && unicode.IsPunct(curr) && !isApostrophe(prev, curr, next) && !isExponentSign(prev, curr, next)
}

// isEscapeSplitter returns whether the current rune and next runes indicate
// the current rune splits words as an escape sequence consuming width bytes.
// \x, \u and \U rune literals do not split words.
func isEscapeSplitter(r rune, next []byte, doubleQuoted bool) (width int, ok bool) {
	if r != '\\' || len(next) == 0 {
		return 0, false
	}
	switch next[0] {
	case 'a', 'b', 'f', 'n', 'r', 't', 'v', '\\', '\'', '"':
		return 1, true
	case 'x':
		if len(next) < 2 || isHex(string(next[:2])) {
			return 0, false
		}
		return 3, !doubleQuoted
	case 'u':
		if len(next) < 4 || isHex(string(next[:4])) {
			return 0, false
		}
		return 5, !doubleQuoted
	case 'U':
		if len(next) < 8 || isHex(string(next[:8])) {
			return 0, false
		}
		return 9, !doubleQuoted
	default:
		if len(next) == 3 {
			for _, c := range next {
				if c < '0' || '7' < c {
					return 0, false
				}
			}
			return 3, false
		}
		return 0, !doubleQuoted
	}
}

// isApostrophe returns whether the current rune is an apostrophe. The heuristic
// used is fairly simple and may not cover all cases correctly, but should handle
// what we want here.
func isApostrophe(last, curr rune, data []byte) bool {
	if curr != '\'' {
		return false
	}
	next, _ := utf8.DecodeRune(data)
	return unicode.IsLetter(last) && unicode.IsLetter(next)
}

// isExponentSign returns whether the current rune is an an exponent sign, the
// heuristic is that the last rune is an e and the next is a digit.
func isExponentSign(last, curr rune, data []byte) bool {
	if curr != '-' {
		return false
	}
	last |= 'a' - 'A'
	next, _ := utf8.DecodeRune(data)
	return last == 'e' && unicode.IsDigit(next)
}
