// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kortschak/camel"
	"github.com/kortschak/hunspell"
	"golang.org/x/tools/go/packages"
	"mvdan.cc/xurls/v2"
)

// checker implement an AST-walking spell checker.
type checker struct {
	fileset *token.FileSet

	spelling *hunspell.Spell
	camel    camel.Splitter

	show          bool // show the context of a misspelling.
	ignoreUpper   bool // ignore words that are all uppercase.
	ignoreSingle  bool // ignore words that are a single rune.
	ignoreNumbers bool // ignore Go syntax number literals.
	maskURLs      bool // mask URLs before checking.
	camelSplit    bool // split words on camelCase when retrying.
	maxWordLen    int  // ignore words longer than this.
	minNakedHex   int  // ignore words at least this long if only hex digits.

	makeSuggestions int // make suggestions for misspelled words.
	suggested       map[string][]string

	// warn is the decoration for incorrectly spelled words.
	warn func(...interface{}) fmt.Formatter
	// suggest is the decoration for suggested words.
	suggest func(...interface{}) fmt.Formatter

	// misspellings is the number of misspellings found.
	misspellings int

	// misspelled is the complete list of misspelled words
	// found during the check. The words must have had any
	// leading and trailing underscores removed.
	misspelled map[string]bool
}

// check checks the provided text and outputs information about any misspellings
// in the text.
func (c *checker) check(text string, pos token.Pos, where string) {
	sc := bufio.NewScanner(c.textReader(text))
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

		if c.isCorrect(stripUnderscores(word), false) {
			continue
		}
		if !seen[word] {
			p := c.fileset.Position(pos)
			fmt.Printf("%v:%d:%d: %q is misspelled in %s", rel(p.Filename), p.Line, p.Column, word, where)

			if c.makeSuggestions == always || (c.makeSuggestions == once && c.suggested[word] == nil) {
				suggestions, ok := c.suggested[word]
				if !ok {
					suggestions = c.spelling.Suggest(word)
					if c.makeSuggestions == always {
						// Cache suggestions.
						c.suggested[word] = suggestions
					} else {
						// Mark as suggested.
						c.suggested[word] = empty
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
				}
			}
			fmt.Println()
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

// rel returns the wd-relative path for the input if possible.
func rel(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil {
		return path
	}
	return rel
}

// urls is used for masking URLs in check.
var urls = xurls.Strict()

// textReader returns an io.Reader containing the provided text conditioned
// according to the configuration.
func (c *checker) textReader(text string) io.Reader {
	if c.maskURLs {
		masked := urls.ReplaceAllStringFunc(text, func(s string) string {
			return strings.Repeat(" ", len(s))
		})
		return strings.NewReader(masked)
	}
	return strings.NewReader(text)
}

// empty is a word suggestion sentinel indicating that previous suggestion
// has been made.
var empty = []string{}

// isCorrect performs the word correctness checks for checker.
func (c *checker) isCorrect(word string, isRetry bool) bool {
	if c.maxWordLen > 0 && len(word) > c.maxWordLen {
		return true
	}
	if c.ignoreUpper && allUpper(word) {
		return true
	}
	if c.ignoreSingle && utf8.RuneCountInString(word) == 1 {
		return true
	}
	if c.minNakedHex != 0 && len(word) >= c.minNakedHex && isHex(word) {
		return true
	}
	if isHexRune(word) {
		return true
	}
	if c.ignoreNumbers && isNumber(word) {
		return true
	}
	if c.spelling.IsCorrect(word) {
		return true
	}
	if isRetry || c.caseFoldMatch(word) {
		// TODO(kortschak): Consider not adding case-fold
		// matches to the misspelled map.
		c.misspellings++
		if c.misspelled != nil {
			c.misspelled[word] = true
		}
		return false
	}
	var fragments []string
	if c.camelSplit {
		// TODO(kortschak): Allow user-configurable
		// known words for camel case splitting.
		fragments = c.camel.Split(word)
	} else {
		fragments = strings.Split(word, "_")
	}
	for _, frag := range fragments {
		if !c.isCorrect(frag, true) {
			return false
		}
	}
	return true
}

// caseFoldMatch returns whether there is a suggestion for the word that
// is an exact match under case folding. This checks for the common error
// of failing to adjust export visibility of labels in comments.
func (c *checker) caseFoldMatch(word string) bool {
	for _, suggest := range c.spelling.Suggest(word) {
		if strings.EqualFold(suggest, word) {
			return true
		}
	}
	return false
}

// Visit walks the AST performing spell checking on any string literals.
func (c *checker) Visit(n ast.Node) ast.Visitor {
	if n, ok := n.(*ast.BasicLit); ok && n.Kind == token.STRING {
		c.check(n.Value, n.Pos(), "string")
	}
	return c
}

// addIdentifiers adds identifier labels to the spelling dictionary.
func addIdentifiers(spelling *hunspell.Spell, pkgs []*packages.Package, seen map[string]bool) error {
	v := &adder{spelling: spelling}
	for _, p := range pkgs {
		v.pkg = p
		for _, e := range strings.Split(p.String(), "/") {
			if !spelling.IsCorrect(e) {
				spelling.Add(e)
			}
		}
		for _, w := range directiveWords(p.Syntax, p.Fset) {
			if !spelling.IsCorrect(w) {
				spelling.Add(w)
			}
		}
		for _, f := range p.Syntax {
			ast.Walk(v, f)
		}
		for _, dep := range p.Imports {
			if seen[dep.String()] {
				continue
			}
			seen[dep.String()] = true
			addIdentifiers(spelling, []*packages.Package{dep}, seen)
		}
	}
	if v.failed != 0 {
		return errors.New("missed adding %d identifiers")
	}
	return nil
}

// directiveWords returns words used in directive comments.
func directiveWords(files []*ast.File, fset *token.FileSet) []string {
	var words []string
	for _, f := range files {
		m := ast.NewCommentMap(fset, f, f.Comments)
		for _, g := range m {
			for _, cg := range g {
				for _, c := range cg.List {
					if !strings.HasPrefix(c.Text, "//") {
						continue
					}
					text := strings.TrimPrefix(c.Text, "//")
					if strings.HasPrefix(text, " ") {
						continue
					}
					idx := strings.Index(text, ":")
					if idx < 1 {
						continue
					}
					if strings.HasPrefix(text[idx+1:], " ") {
						continue
					}
					line := strings.SplitN(text, "\n", 2)[0]
					directive := strings.SplitN(line, " ", 2)[0]
					words = append(words, strings.FieldsFunc(directive, func(r rune) bool {
						return unicode.IsSpace(r) || unicode.IsSymbol(r) || unicode.IsPunct(r)
					})...)
				}
			}
		}
	}
	return words
}

// adder is an ast.Visitor that adds tokens to a spelling dictionary.
type adder struct {
	spelling *hunspell.Spell
	failed   int
	pkg      *packages.Package
}

// Visit adds the names of all identifiers to the dictionary.
func (a *adder) Visit(n ast.Node) ast.Visitor {
	switch n := n.(type) {
	case *ast.Ident:
		// Check whether this is a type and only make it
		// countable in that case.
		ok := n.Obj != nil && n.Obj.Kind == ast.Typ
		a.addWordUnknownWord(stripUnderscores(n.Name), ok)
	case *ast.StructType:
		typ, ok := a.pkg.TypesInfo.Types[n].Type.(*types.Struct)
		if !ok {
			break
		}
		for i := 0; i < typ.NumFields(); i++ {
			f := typ.Field(i)
			if !f.Exported() {
				continue
			}
			for _, w := range extractStructTagWords(typ.Tag(i)) {
				a.addWordUnknownWord(w, false)
			}
		}
	}
	return a
}

func (a *adder) addWordUnknownWord(w string, countable bool) {
	if a.spelling.IsCorrect(w) {
		// Assume we have the correct plurality rules.
		// This should work most of the time. If it turns
		// out to be a problem, we can make this conditional
		// on countable and always add those terms.
		return
	}
	var ok bool
	if countable {
		ok = a.spelling.AddWithAffix(w, "item")
	} else {
		ok = a.spelling.Add(w)
	}
	if !ok {
		a.failed++
	}
}

// stripUnderscores removes leading and trailing underscores from
// words to prevent emph marking used in comments from preventing
// spell check matching.
func stripUnderscores(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == '_' })
}

// join returns the string join of the given args.
func join(args []interface{}) string {
	var buf strings.Builder
	for _, a := range args {
		fmt.Fprint(&buf, a)
	}
	return buf.String()
}
