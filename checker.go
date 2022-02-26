// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/kortschak/camel"
	"github.com/kortschak/ct"
	"mvdan.cc/xurls/v2"
)

// checker implements an AST-walking spell checker.
type checker struct {
	fileset *token.FileSet

	dictionary *dictionary
	camel      camel.Splitter
	heuristics []heuristic

	config

	misspellings []misspelling

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
func (c *checker) check(text string, node ast.Node, where string) (ok bool) {
	sc := bufio.NewScanner(c.textReader(text))
	w := words{}
	sc.Split(w.ScanWords)

	var words []misspelled
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
		words = append(words, misspelled{word: word, span: w.current})
	}
	if len(words) != 0 {
		c.misspellings = append(c.misspellings, misspelling{
			words: words,
			where: where,
			text:  text,
			pos:   c.fileset.Position(node.Pos()),
			end:   c.fileset.Position(node.End()),
		})
	}
	return len(words) == 0
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
		c.check(n.Value, n, "string")
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

// stripUnderscores removes leading and trailing underscores from
// words to prevent emph marking used in comments from preventing
// spell check matching.
func stripUnderscores(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == '_' })
}
