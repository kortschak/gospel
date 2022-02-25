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
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/kortschak/camel"
	"github.com/kortschak/ct"
	"github.com/kortschak/hunspell"
	"golang.org/x/tools/go/packages"
	"mvdan.cc/xurls/v2"
)

// checker implements an AST-walking spell checker.
type checker struct {
	fileset *token.FileSet

	dictionary *dictionary
	camel      camel.Splitter
	heuristics []heuristic

	config

	suggested map[string][]string

	// warn is the decoration for incorrectly spelled words.
	warn func(...interface{}) fmt.Formatter
	// suggest is the decoration for suggested words.
	suggest func(...interface{}) fmt.Formatter
}

// newChecker returns a new spelling checker using the provided spelling
// and configuration.
func newChecker(d *dictionary, cfg config) (*checker, error) {
	c := &checker{
		dictionary: d,
		config:     cfg,
		camel:      camel.NewSplitter([]string{"\\"}),
		heuristics: []heuristic{
			wordLen{cfg.MaxWordLen},
			isNakedHex{cfg.MinNakedHex},
			isHexRune{},
			isUnit{},
		},
		warn: (ct.Italic | ct.Fg(ct.BoldRed)).Paint,
	}
	if c.Show {
		c.suggest = (ct.Italic | ct.Fg(ct.BoldGreen)).Paint
	} else {
		c.suggest = ct.Mode(0).Paint
	}
	if c.MakeSuggestions != never {
		c.suggested = make(map[string][]string)
	}

	// Add optional heuristics.
	if c.IgnoreUpper {
		c.heuristics = append(c.heuristics, allUpper{})
	}
	if c.IgnoreSingle {
		c.heuristics = append(c.heuristics, isSingle{})
	}
	if c.IgnoreNumbers {
		c.heuristics = append(c.heuristics, &isNumber{})
	}
	if len(c.Patterns) != 0 {
		p, err := newPatterns(c.Patterns)
		if err != nil {
			return nil, err
		}
		c.heuristics = append(c.heuristics, p)
	}

	return c, nil
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

			if c.MakeSuggestions == always || (c.MakeSuggestions == once && c.suggested[word] == nil) {
				suggestions, ok := c.suggested[word]
				if !ok {
					suggestions = c.dictionary.Suggest(word)
					if c.MakeSuggestions == always {
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
		if c.Show {
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
	if c.MaskURLs {
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
func (c *checker) isCorrect(word string, partial bool) bool {
	for _, h := range c.heuristics {
		if h.isAcceptable(word, partial) {
			return true
		}
	}
	if c.dictionary.IsCorrect(word) {
		return true
	}
	if partial || c.caseFoldMatch(word) {
		// TODO(kortschak): Consider not adding case-fold
		// matches to the misspelled map.
		c.dictionary.noteMisspelling(word)
		return false
	}
	var fragments []string
	if c.CamelSplit {
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
	for _, suggest := range c.dictionary.Suggest(word) {
		if strings.EqualFold(suggest, word) {
			return true
		}
	}
	return false
}

// Visit walks the AST performing spell checking on any string literals.
func (c *checker) Visit(n ast.Node) ast.Visitor {
	if n, ok := n.(*ast.BasicLit); ok && n.Kind == token.STRING {
		isDoubleQuoted := n.Value[0] == '"'
		text := n.Value
		if isDoubleQuoted {
			var err error
			text, err = strconv.Unquote(text)
			if err != nil {
				// This should never happen.
				isDoubleQuoted = false
				text = n.Value
			}
		}
		if c.unexpectedEntropy(text, isDoubleQuoted) {
			return c
		}
		c.check(n.Value, n.Pos(), "string")
	}
	return c
}

// unexpectedEntropy returns whether the text falls outside the expected
// ranges for text.
func (c *checker) unexpectedEntropy(text string, print bool) bool {
	if !c.EntropyFiler.Filter || len(text) < c.EntropyFiler.MinLenFiltered {
		return false
	}
	e := entropy(text, print)
	low := expectedEntropy(len(text), c.EntropyFiler.Accept.Low)
	high := expectedEntropy(len(text), c.EntropyFiler.Accept.High)
	return e < low || high < e
}

// expectedEntropy returns the expected entropy for a sequence of n letters
// uniformly chosen from an alphabet of s letters.
func expectedEntropy(n, s int) float64 {
	if n > s {
		n = s
	}
	if n < 2 {
		return 0
	}
	return -math.Log2(1 / float64(n))
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

// entropy returns the entropy of the provided text in bits. If
// print is true, non-printable characters are grouped into a single
// class.
func entropy(text string, print bool) float64 {
	if text == "" {
		return 0
	}

	var counts [256]float64
	for _, b := range []byte(text) {
		if print && !unicode.IsPrint(rune(b)) {
			continue
		}
		counts[b]++
	}
	n := len(text)

	// e = -∑i=1..k((p_i)*log(p_i))
	var e float64
	for _, cnt := range counts {
		if cnt == 0 {
			// Ignore zero counts.
			continue
		}
		p := cnt / float64(n)
		e += p * math.Log2(p)
	}
	if e == 0 {
		// Don't negate zero.
		return 0
	}
	return -e
}
