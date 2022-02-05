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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kortschak/ct"
	"github.com/kortschak/hunspell"
	"golang.org/x/tools/go/packages"
)

func main() { os.Exit(gospel()) }

func gospel() int {
	show := flag.Bool("show", true, "print comment or string with misspellings")
	checkStrings := flag.Bool("check-strings", false, "check string literals")
	ignoreUpper := flag.Bool("ignore-upper", true, "ignore all-uppercase words")
	ignoreSingle := flag.Bool("ignore-single", true, "ignore single letter words")
	ignoreIdents := flag.Bool("ignore-idents", true, "ignore words matching identifiers")
	words := flag.String("misspellings", "", "file to write a dictionary of misspellings (.dic format)")
	update := flag.Bool("update-dict", true, "update misspellings dictionary instead of creating a new one")
	lang := flag.String("lang", "en_US", "language to use")
	dicts := flag.String("dict-paths", path, "directory list containing hunspell dictionaries")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `usage: %s [options] [packages]

The gospel program will report misspellings in Go source comments and strings.

The position of each comment block or string with misspelled a word will be
output. If the -show flag is true, the complete comment block or string will
be printed with misspelled words highlighted.

If files with the name ".words" exist at module roots, they are loaded as
dictionaries unless the misspellings flag is set. The ".words" file is read
as a hunspell .dic format file and so requires a non-zero numeric value on
the first line. This value is a hint to hunspell for the number of words in
the dictionary and is populated correctly by the misspellings option. The
file may be edited to remove incorrect words without requiring the hint to
be adjusted.

`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *lang == "" {
		fmt.Fprintln(os.Stderr, "missing lang flag")
		return 2
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
				return 1
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
		return 1
	}
	for _, w := range knownWords {
		spelling.Add(w)
	}

	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedDeps | packages.NeedModule,
	}
	pkgs, err := packages.Load(cfg, flag.Args()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return 1
	}
	if packages.PrintErrors(pkgs) != 0 {
		return 1
	}

	if *ignoreIdents {
		err = addIdentifiers(spelling, pkgs)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	// Load any dictionaries that exist in well known locations
	// at module roots. We do not do this when we are outputting
	// a misspelling list since the list will be incomplete unless
	// it is appended to the existing list, unless we are making
	// and updated dictionary when we will merge them.
	if *words == "" || *update {
		roots := make(map[string]bool)
		for _, p := range pkgs {
			roots[p.Module.Dir] = true
		}
		for r := range roots {
			err := spelling.AddDict(filepath.Join(r, ".words"))
			if _, ok := err.(*os.PathError); !ok && err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}

	c := &checker{
		spelling:     spelling,
		show:         *show,
		ignoreUpper:  *ignoreUpper,
		ignoreSingle: *ignoreSingle,
		warn:         (ct.Italic | ct.Fg(ct.BoldRed)).Paint,
		misspelled:   make(map[string]bool),
	}
	if *words != "" {
		c.misspelled = make(map[string]bool)
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

	// Write out a dictionary of the misspelled words.
	// The hunspell .dic format includes a count hint
	// at the top of the file so add that as well.
	if *words != "" {
		if *update {
			// Carry over words from the already existing dictionary.
			old, err := os.Open(".words")
			if err == nil {
				sc := bufio.NewScanner(old)
				for i := 0; sc.Scan(); i++ {
					if i == 0 {
						continue
					}
					c.misspelled[sc.Text()] = true
				}
				old.Close()
			} else if !errors.Is(err, fs.ErrNotExist) {
				fmt.Fprintf(os.Stderr, "failed to open .words file: %v", err)
				return 1

			}
		}

		f, err := os.Create(*words)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open misspellings file: %v", err)
			return 1
		}
		defer f.Close()
		dict := make([]string, 0, len(c.misspelled))
		for m := range c.misspelled {
			dict = append(dict, m)
		}
		sort.Strings(dict)
		fmt.Fprintln(f, len(dict))
		for _, m := range dict {
			fmt.Fprintln(f, m)
		}
	}

	return 0
}

// checker implement an AST-walking spell checker.
type checker struct {
	fileset *token.FileSet

	spelling *hunspell.Spell

	show         bool // show the context of a misspelling.
	ignoreUpper  bool // ignore words that are all uppercase.
	ignoreSingle bool // ignore words that are a single rune.

	// warn is the decoration for incorrectly spelled words.
	warn func(...interface{}) fmt.Formatter

	// misspelled is the complete list of misspelled words
	// found during the check. The words must have had any
	// leading and trailing underscores removed.
	misspelled map[string]bool
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

		strippedWord := stripUnderscores(word)
		if c.ignoreUpper && allUpper(strippedWord) {
			continue
		}
		if c.ignoreSingle && utf8.RuneCountInString(strippedWord) == 1 {
			continue
		}
		if c.spelling.IsCorrect(strippedWord) {
			continue
		}
		if c.misspelled != nil {
			c.misspelled[strippedWord] = true
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
