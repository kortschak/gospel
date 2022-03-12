// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"go/token"
	"sort"
	"strings"
)

// misspelling is an identified misspelled word and its position.
type misspelling struct {
	text  string
	where string
	pos   token.Position
	end   token.Position
	words []misspelled
}

// misspelled is a misspelled word and its span.
type misspelled struct {
	word    string
	span    span
	note    string
	suggest bool
}

// adjacent returns whether the receiver is on an adjacent line to
// prev.
func (m misspelling) adjacent(prev misspelling) bool {
	return m.pos.Filename == prev.pos.Filename &&
		m.pos.Line-prev.end.Line <= 1
}

// report writes a report to stdout.
func (c *checker) report() {
	sort.Slice(c.misspellings, func(i, j int) bool {
		mi := c.misspellings[i]
		mj := c.misspellings[j]
		switch {
		case mi.pos.Filename < mj.pos.Filename:
			return true
		case mi.pos.Filename > mj.pos.Filename:
			return false
		default:
			return mi.pos.Offset < mj.pos.Offset
		}
	})

	var (
		chunks  [][]misspelling
		current []misspelling
	)
	for i, m := range c.misspellings {
		if i != 0 && !m.adjacent(c.misspellings[i-1]) {
			chunks = append(chunks, current)
			current = nil
		}
		current = append(current, m)
	}
	if current != nil {
		chunks = append(chunks, current)
	}

	for _, chunk := range chunks {
		suggested := make(map[string]bool)
		for _, l := range chunk {
			for _, w := range l.words {
				p := l.pos
				if p.IsValid() {
					var generated string
					if c.generated[p.Filename] {
						generated = " (generated file)"
					}
					fmt.Printf("%v:%d:%d: %q is %s in %s%s", rel(p.Filename), p.Line, p.Column+w.span.pos, w.word, w.note, l.where, generated)
				} else {
					fmt.Printf("%v@%d: %q is %s in %s", rel(p.Filename), w.span.pos, w.word, w.note, l.where)
				}

				if w.suggest &&
					(c.MakeSuggestions == always ||
						(c.MakeSuggestions == each && !suggested[w.word]) ||
						(c.MakeSuggestions == once && c.suggested[w.word] == nil)) {
					suggestions, ok := c.suggested[w.word]
					if !ok {
						suggestions = c.dictionary.Suggest(w.word)
						switch c.MakeSuggestions {
						case always, each:
							// Cache suggestions.
							c.suggested[w.word] = suggestions
						default:
							// Mark as suggested.
							c.suggested[w.word] = empty
						}
					}
					if len(suggestions) != 0 {
						fmt.Print(" (suggest: ")
						for i, s := range suggestions {
							if i != 0 {
								fmt.Print(", ")
							}
							fmt.Printf("%s", c.suggest(s))
						}
						fmt.Print(")")
						if c.MakeSuggestions == each {
							suggested[w.word] = true
						}
					}
				}
				fmt.Println()
			}
		}

		if c.Show {
			for i, l := range chunk {
				if !l.pos.IsValid() {
					// Don't try to print binary embedded data.
					continue
				}

				if len(l.words) == 0 {
					if i == 0 || l.text != chunk[i-1].text {
						fmt.Print(adjustIndents(l.text))
					}
					continue
				}
				var (
					args    []interface{}
					lastPos int
				)
				generated := c.generated[l.pos.Filename]
				for _, w := range l.words {
					if w.span.pos != lastPos {
						args = append(args, l.text[lastPos:w.span.pos])
					}
					args = append(args, c.warn[generated](l.text[w.span.pos:w.span.pos+len(w.word)]), l.text[w.span.pos+len(w.word):w.span.end])
					lastPos = w.span.end
				}
				if lastPos != len(l.text) {
					args = append(args, l.text[lastPos:])
				}

				if args != nil {
					fmt.Print(adjustIndents(join(args)))
				}
			}
		}
	}
}

// join returns the string join of the given args.
func join(args []interface{}) string {
	var buf strings.Builder
	for _, a := range args {
		fmt.Fprint(&buf, a)
	}
	return buf.String()
}

// adjustIndents adjusts indents to that all blocks are indented a single
// tab.
func adjustIndents(s string) string {
	indent := indentLevel(s)
	lines := strings.Split(s, "\n")
	var buf strings.Builder
	for i, l := range lines {
		if l == "" {
			continue
		}
		if i != 0 {
			l = l[indent:]
		}
		fmt.Fprintf(&buf, "\t%s\n", l)
	}
	return buf.String()
}

// indentLevel returns the indent level of a block comment. It returns
// zero if chunk is not a block comment.
func indentLevel(chunk string) int {
	if !strings.HasPrefix(chunk, "/*") {
		return 0
	}
	lastLine := strings.LastIndex(chunk, "\n")
	if lastLine < 0 {
		return 0
	}
	// Assume correctly formatted code with tab indentation.
	for i, r := range chunk[lastLine+1:] {
		if r != '\t' {
			return i
		}
	}
	return -1
}
