// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"go/token"
	"os"
	"sort"
	"unicode"
	"unicode/utf8"
)

// embedded is a representation of embedded data.
type embedded struct {
	path  string
	data  string
	lines []int
}

// loadEmbedded reads the file at the provided path as an embedded.
// If the data in the file is not valid UTF-8, contains bytes not found
// in ASCII or UTF-8 text, or contains lines longer than maxLineLen, no
// line-based position information will be retained and the file will be
// treated as binary data.
func (c *checker) loadEmbedded(path string, maxLineLen int) (*embedded, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	e := &embedded{path: path, data: string(b)}
	if c.unexpectedEntropy(e.data, false) { // Consider all characters for entropy.
		e.data = ""
		return e, nil
	}
	if !utf8.ValidString(e.data) {
		return e, nil
	}
	e.lines = []int{0}
	for i, b := range e.data {
		if (b <= unicode.MaxASCII && neverInText[b]) || i > e.lines[len(e.lines)-1]+maxLineLen {
			e.lines = nil
			break
		}
		if b == '\n' {
			e.lines = append(e.lines, i)
		}
	}
	return e, nil
}

// neverInText is the set of bytes never found in ASCII/UTF-8 text files.
var neverInText = [256]bool{
	// First row minus BEL BS TAB LF VT FF CR.
	0x00: true, 0x01: true, 0x02: true, 0x03: true, 0x04: true,
	0x05: true, 0x06: true, 0x0e: true, 0x0f: true,

	// Second row minus ESC.
	0x10: true, 0x11: true, 0x12: true, 0x13: true, 0x14: true,
	0x15: true, 0x16: true, 0x17: true, 0x18: true, 0x19: true,
	0x1a: true, 0x1c: true, 0x1d: true, 0x1e: true, 0x1f: true,

	// DEL.
	0x7f: true,
}

// Text returns the text representation of the embedded data.
func (e *embedded) Text() string { return e.data }

// Pos and End implement ast.Node.
func (e *embedded) Pos() token.Pos { return 1 }
func (e *embedded) End() token.Pos { return e.Pos() + token.Pos(len(e.data)) }

// Position implements positioner.
func (e *embedded) Position(pos token.Pos) token.Position {
	p := int(pos)
	var line, col int
	if e.lines != nil && pos.IsValid() && p <= len(e.data) {
		line = sort.SearchInts(e.lines, p)
		col = p - e.lines[line-1]
	}
	return token.Position{
		Filename: e.path,
		Offset:   p,
		Line:     line,
		Column:   col,
	}
}
