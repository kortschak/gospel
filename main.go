// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The gospel command finds and highlights misspelled words in Go source
// comments and strings. It uses hunspell to identify misspellings and only
// emits coloured output for visual inspection; don't use it in automated
// linting.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kortschak/ct"
	"github.com/kortschak/hunspell"
	"golang.org/x/tools/go/packages"
)

func main() {
	show := flag.Bool("show", true, "print comment or string with misspellings")
	checkStrings := flag.Bool("check-strings", false, "check string literals")
	ignoreUpper := flag.Bool("ignore-upper", true, "ignore all-uppercase words")
	ignoreIdents := flag.Bool("ignore-idents", true, "ignore words matching identifiers")
	lang := flag.String("lang", "en_US", "language to use")
	dicts := flag.String("dict-paths", path, "directory list containing hunspell dictionaries")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `usage: %s [options] [packages]

The gospel program will report misspellings in Go source comments and strings.

The position of each comment block or string with misspelled a word will be
output. If the -show flag is true, the complete comment block or string will
be printed with misspelled words highlighted.

`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *lang == "" {
		fmt.Fprintf(os.Stderr, "missing lang flag")
		os.Exit(1)
	}
	var (
		spelling *hunspell.Spell
		err      error
	)
	for _, p := range filepath.SplitList(*dicts) {
		if strings.HasPrefix(p, "~"+string(filepath.Separator)) {
			dir, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not expand tilde: %v\n", err)
				os.Exit(1)
			}
			p = filepath.Join(dir, p[2:])
		}
		spelling, err = hunspell.NewSpell(p, *lang)
		if err == nil {
			break
		}
	}
	if spelling == nil {
		fmt.Fprintf(os.Stderr, "no %s dictionary found in: %v\n", *lang, *dicts)
		os.Exit(1)
	}
	for _, w := range knownWords {
		spelling.Add(w)
	}

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

	if *ignoreIdents {
		err = addIdentifiers(spelling, pkgs)
		if err != nil {
			log.Fatal(err)
		}
	}

	c := &checker{
		spelling:    spelling,
		show:        *show,
		ignoreUpper: *ignoreUpper,
		warn:        (ct.Italic | ct.Fg(ct.BoldRed)).Paint,
	}
	for _, p := range pkgs {
		c.fileset = p.Fset
		for _, f := range p.Syntax {
			if *checkStrings {
				ast.Walk(c, f)
			}
			for _, g := range f.Comments {
				c.check(g.Text(), g.Pos(), "comment")
			}
		}
	}
}

// checker implement an AST-walking spell checker.
type checker struct {
	fileset *token.FileSet

	spelling *hunspell.Spell

	show        bool // show the context of a misspelling.
	ignoreUpper bool // ignore words that are all uppercase.

	// warn is the decoration for incorrectly spelled words.
	warn func(...interface{}) fmt.Formatter
}

// check checks the provided text and outputs information about any misspellings
// in the text.
func (c *checker) check(text string, pos token.Pos, where string) {
	sc := bufio.NewScanner(strings.NewReader(text))
	w := words{}
	sc.Split(w.ScanWords)

	var (
		args    []interface{}
		lastPos int
	)
	seen := make(map[string]bool)
	for sc.Scan() {
		word := sc.Text()

		// Remove common suffixes from words.
		// Note that prefix removal cannot be
		// done without adjusting the word's
		// start position.
		switch {
		case strings.HasSuffix(word, "'s"):
			word = strings.TrimSuffix(word, "'s")
		case strings.HasSuffix(word, "'d"):
			word = strings.TrimSuffix(word, "'d")
		case strings.HasSuffix(word, "'ed"):
			word = strings.TrimSuffix(word, "'ed")
		case strings.HasSuffix(word, "'th"):
			word = strings.TrimSuffix(word, "'th")
		}

		if (c.ignoreUpper && allUpper(word)) || c.spelling.IsCorrect(stripUnderscores(word)) {
			continue
		}
		if !seen[word] {
			fmt.Printf("%v: %q is misspelled in %s\n", c.fileset.Position(pos), word, where)
			seen[word] = true
		}
		if c.show {
			if w.current.pos != lastPos {
				args = append(args, text[lastPos:w.current.pos])
			}
			args = append(args, c.warn(text[w.current.pos:w.current.pos+len(word)]), text[w.current.pos+len(word):w.current.end])
			lastPos = w.current.end
		}
	}
	if args != nil {
		if lastPos != len(text) {
			args = append(args, text[lastPos:])
		}
		lines := strings.Split(join(args), "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		fmt.Printf("\t%s\n", strings.Join(lines, "\n\t"))
	}
}

// Visit walks the AST performing spell checking on any string literals.
func (c *checker) Visit(n ast.Node) ast.Visitor {
	if n, ok := n.(*ast.BasicLit); ok && n.Kind == token.STRING {
		c.check(n.Value, n.Pos(), "string")
	}
	return c
}

// addIdentifiers adds identifier labels to the spelling dictionary.
func addIdentifiers(spelling *hunspell.Spell, pkgs []*packages.Package) error {
	v := &adder{spelling: spelling}
	for _, p := range pkgs {
		for _, e := range strings.Split(p.String(), "/") {
			spelling.Add(e)
		}
		for _, f := range p.Syntax {
			ast.Walk(v, f)
		}
	}
	if v.failed != 0 {
		return errors.New("missed adding %d identifiers")
	}
	return nil
}

// adder is an ast.Visitor that adds tokens to a spelling dictionary.
type adder struct {
	spelling *hunspell.Spell
	failed   int
}

// Visit adds the names of all identifiers to the dictionary.
func (a *adder) Visit(n ast.Node) ast.Visitor {
	if n, ok := n.(*ast.Ident); ok {
		ok = a.spelling.Add(stripUnderscores(n.Name))
		if !ok {
			a.failed++
		}
	}
	return a
}

// stripUnderscores removes leading and trailing underscores from
// words to prevent emph marking used in comments from preventing
// spell check matching.
func stripUnderscores(s string) string {
	return strings.TrimPrefix(strings.TrimSuffix(s, "_"), "_")
}

// allUpper returns whether all runes in s are uppercase. For the purposed
// of this test, numerals and underscores are considered uppercase. As a
// special case, a final 's' is also considered uppercase to allow plurals
// of initialisms and acronyms.
func allUpper(s string) bool {
	s = strings.TrimSuffix(s, "s")
	for _, r := range s {
		if !unicode.IsUpper(r) && !unicode.IsDigit(r) && r != '_' {
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
	var prev rune
	for width, i := 0, 0; start < len(data); start += width {
		var r rune
		r, width = utf8.DecodeRune(data[start:])
		if !isSplitter(prev, r, data[i+width:]) {
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
	return curr != '_' && unicode.IsPunct(curr) && !isApostrophe(prev, curr, next)
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
