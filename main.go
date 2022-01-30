// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The gospel command finds and highlights misspelled words in Go source
// comments. It uses hunspell to identify misspellings and only emits
// coloured output for visual inspection; don't use it in automated linting.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kortschak/ct"
	"github.com/kortschak/hunspell"
	"golang.org/x/tools/go/packages"
)

func main() {
	show := flag.Bool("show", true, "print comment with repeats")
	ignoreUpper := flag.Bool("ignore-upper", true, "ignore all-uppercase words")
	lang := flag.String("lang", "en_US", "language to use")
	dicts := flag.String("dict-path", "/usr/share/hunspell", "directory containing hunspell dictionaries")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `usage: %s [options] [packages]

The gospel program will report misspellings in Go source comments.

Each comment block with misspelled word will be output for each word and
if the -show flag is true, the complete comment block will be printed with
misspelled words highlighted.

`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	spelling, err := hunspell.NewSpell(*dicts, *lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dictionaries: %v\n", err)
		os.Exit(1)
	}
	keywords := []string{
		"break", "case", "chan", "const", "continue", "default",
		"defer", "else", "fallthrough", "for", "func", "go", "goto",
		"if", "import", "interface", "map", "package", "range",
		"return", "select", "struct", "switch", "type", "var",
	}
	for _, w := range keywords {
		spelling.Add(w)
	}

	// TODO(kortschak): First walk the AST to add all identifiers to the
	// run-time dictionary so we don't mark those words as misspelled.

	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedDeps,
	}
	pkgs, err := packages.Load(cfg, flag.Args()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}
	if packages.PrintErrors(pkgs) != 0 {
		os.Exit(1)
	}

	warn := (ct.Italic | ct.Fg(ct.BoldRed)).Paint

	for _, p := range pkgs {
		for _, f := range p.Syntax {
			for _, c := range f.Comments {
				comment := c.Text()
				sc := bufio.NewScanner(strings.NewReader(comment))
				w := words{}
				sc.Split(w.ScanWords)

				var (
					args    []interface{}
					lastPos int
				)
				seen := make(map[string]bool)
				for sc.Scan() {
					text := sc.Text()

					if !(*ignoreUpper && allUpper(text)) && !spelling.IsCorrect(text) {
						if !seen[text] {
							fmt.Printf("%v: %q is misspelled\n", p.Fset.Position(c.Pos()), text)
							seen[text] = true
						}
						if *show {
							if w.current.pos != lastPos {
								args = append(args, comment[lastPos:w.current.pos])
							}
							args = append(args, warn(comment[w.current.pos:w.current.pos+len(text)]), comment[w.current.pos+len(text):w.current.end])
							lastPos = w.current.end
						}
					}
				}
				if args != nil {
					if lastPos != len(comment) {
						args = append(args, comment[lastPos:])
					}
					lines := strings.Split(join(args), "\n")
					if lines[len(lines)-1] == "" {
						lines = lines[:len(lines)-1]
					}
					fmt.Printf("\t%s\n", strings.Join(lines, "\n\t"))
				}
			}
		}
	}
}

// allUpper returns whether all runes in s are uppercase.
func allUpper(s string) bool {
	for _, r := range s {
		if !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// join returns the string join of the given args.
func join(args []interface{}) string {
	var buf strings.Builder
	for _, a := range args {
		fmt.Fprint(&buf, a)
	}
	return buf.String()
}

// words provides a word scanner for bufio.Scanner that can report the
// position of the last found word in the scanner source.
type words struct {
	current span
}

type span struct {
	pos, end int
}

// ScanWords is derived from the bufio.ScanWords split functions.
//
// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// Skip leading spaces.

// ScanWords is a split function for a Scanner that returns each
// space/punctuation-separated word of text, with surrounding spaces
// deleted. It will never return an empty string. The definition of
// space/punctuation is set by unicode.IsSpace and unicode.IsPunct.
func (w *words) ScanWords(data []byte, atEOF bool) (advance int, token []byte, err error) {
	start := 0
	w.current.pos = w.current.end
	for width := 0; start < len(data); start += width {
		var r rune
		r, width = utf8.DecodeRune(data[start:])
		if !unicode.IsSpace(r) && !unicode.IsPunct(r) {
			break
		}
	}
	w.current.pos += start

	// Scan until space, marking end of word.
	for width, i := 0, start; i < len(data); i += width {
		var r rune
		r, width = utf8.DecodeRune(data[i:])
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			w.current.end += i + width
			return i + width, data[start:i], nil
		}
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
