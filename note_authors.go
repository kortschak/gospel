// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"go/ast"
	"regexp"
	"strings"

	"github.com/kortschak/hunspell"
)

// addNoteAuthors is derived from the go/doc readNotes function.
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

var (
	noteMarker    = `([A-Z][A-Z]+)\(([^)]+)\):?`                    // MARKER(uid), MARKER at least 2 chars, uid at least 1 char
	noteMarkerRx  = regexp.MustCompile(`^[ \t]*` + noteMarker)      // MARKER(uid) at text start
	noteCommentRx = regexp.MustCompile(`^/[/*][ \t]*` + noteMarker) // MARKER(uid) at comment start
)

// addNoteAuthors extracts note author names from comments.
// A note must start at the beginning of a comment with "MARKER(uid):"
// and is followed by the note body (e.g., "// BUG(kortschak): fix this").
// The note ends at the end of the comment group or at the start of
// another note in the same comment group, whichever comes first.
func addNoteAuthors(spelling *hunspell.Spell, comments []*ast.CommentGroup) {
	for _, g := range comments {
		i := -1 // comment index of most recent note start, valid if >= 0
		for j, c := range g.List {
			if noteCommentRx.MatchString(c.Text) {
				if i >= 0 {
					readNote(spelling, g.List[i:j])
				}
				i = j
			}
		}
		if i >= 0 {
			readNote(spelling, g.List[i:])
		}
	}
}

// readNote collects a single note from a sequence of comments.
func readNote(spelling *hunspell.Spell, list []*ast.Comment) {
	text := (&ast.CommentGroup{List: list}).Text()
	if m := noteMarkerRx.FindStringSubmatchIndex(text); m != nil {
		if strings.TrimSpace(text[m[1]:]) != "" {
			sc := bufio.NewScanner(strings.NewReader(text[m[4]:m[5]]))
			var w words // Use our word scanner to retain parity.
			sc.Split(w.ScanWords)
			for sc.Scan() {
				spelling.Add(sc.Text())
			}
		}
	}
}
