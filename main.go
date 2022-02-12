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
	"go/scanner"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kortschak/camel"
	"github.com/kortschak/ct"
	"github.com/kortschak/hunspell"
	"golang.org/x/tools/go/packages"
)

func main() { os.Exit(gospel()) }

// Exit status codes.
const (
	success       = 0
	internalError = 1 << iota
	invocationError
	directiveError // Currently unused. This will be for linting directives.
	spellingError
)

// Suggestion behaviour.
const (
	never  = 0
	once   = 1
	always = 2
)

func gospel() (status int) {
	show := flag.Bool("show", true, "print comment or string with misspellings")
	checkStrings := flag.Bool("check-strings", false, "check string literals")
	ignoreUpper := flag.Bool("ignore-upper", true, "ignore all-uppercase words")
	ignoreSingle := flag.Bool("ignore-single", true, "ignore single letter words")
	ignoreIdents := flag.Bool("ignore-idents", true, "ignore words matching identifiers")
	ignoreNumbers := flag.Bool("ignore-numbers", true, "ignore Go syntax number literals")
	camelSplit := flag.Bool("camel", true, "split words on camel case")
	minNakedHex := flag.Int("min-naked-hex", 8, "length to recognize hex-digit words as number (0 is never ignore)")
	maxWordLen := flag.Int("max-word-len", 40, "ignore words longer than this (0 is no limit)")
	suggest := flag.Int("suggest", 0, "make suggestions for misspellings (0 - never, 1 - first instance, 2 - always)")
	words := flag.String("misspellings", "", "file to write a dictionary of misspellings (.dic format)")
	update := flag.Bool("update-dict", false, "update misspellings dictionary instead of creating a new one")
	lang := flag.String("lang", "en_US", "language to use")
	dicts := flag.String("dict-paths", path, "directory list containing hunspell dictionaries")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `usage: %s [options] [packages]

The gospel program will report misspellings in Go source comments and strings.

The position of each comment block or string with misspelled a word will be
output. If the -show flag is true, the complete comment block or string will
be printed with misspelled words highlighted.

If files with the name ".words" exist at module roots, they are loaded as
dictionaries unless the misspellings flag is set without update-dict.
The ".words" file is read as a hunspell .dic format file and so requires
a non-zero numeric value on the first line. This value is a hint to hunspell
for the number of words in the dictionary and is populated correctly by the
misspellings option. The file may be edited to remove incorrect words without
requiring the hint to be adjusted.

`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *lang == "" {
		fmt.Fprintln(os.Stderr, "missing lang flag")
		return invocationError
	}
	if *suggest < never || always < *suggest {
		fmt.Fprintln(os.Stderr, "invalid suggest flag value")
		return invocationError
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
		return internalError
	}

	cfg := &packages.Config{
		Mode: packages.NeedFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedModule,
	}
	pkgs, err := packages.Load(cfg, flag.Args()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		return internalError
	}
	if packages.PrintErrors(pkgs) != 0 {
		return internalError
	}

	// Load known words as a dictionary. This requires a write to
	// disk since hunspell does not allow dictionaries to be loaded
	// from memory, and affix rules can't be provided directly.
	kw, err := os.CreateTemp("", "gospel")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create known words dictionary: %v", err)
		return internalError
	} else {
		defer os.Remove(kw.Name())
		fmt.Fprintln(kw, len(knownWords))
		for _, w := range knownWords {
			fmt.Fprintln(kw, w)
		}
		err := spelling.AddDict(kw.Name())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return internalError
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
			if p.Module == nil {
				continue
			}
			roots[p.Module.Dir] = true
		}
		for r := range roots {
			err := spelling.AddDict(filepath.Join(r, ".words"))
			if _, ok := err.(*os.PathError); !ok && err != nil {
				fmt.Fprintln(os.Stderr, err)
				return internalError
			}
		}
	}

	if *ignoreIdents {
		err = addIdentifiers(spelling, pkgs, make(map[string]bool))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return internalError
		}
	}

	// Add authors identifiers gleaned from NOTEs.
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			addNoteAuthors(spelling, f.Comments)
		}
	}

	c := &checker{
		spelling:        spelling,
		show:            *show,
		ignoreUpper:     *ignoreUpper,
		ignoreSingle:    *ignoreSingle,
		ignoreNumbers:   *ignoreNumbers,
		camelSplit:      *camelSplit,
		maxWordLen:      *maxWordLen,
		minNakedHex:     *minNakedHex,
		makeSuggestions: *suggest,
		warn:            (ct.Italic | ct.Fg(ct.BoldRed)).Paint,
	}
	if c.show {
		c.suggest = (ct.Italic | ct.Fg(ct.BoldGreen)).Paint
	} else {
		c.suggest = ct.Mode(0).Paint
	}
	if *words != "" {
		c.misspelled = make(map[string]bool)
	}
	if *suggest != never {
		c.suggested = make(map[string][]string)
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
	if c.misspellings != 0 {
		status |= spellingError
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
				return internalError
			}
		}

		f, err := os.Create(*words)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open misspellings file: %v", err)
			return internalError
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

	return status
}

// checker implement an AST-walking spell checker.
type checker struct {
	fileset *token.FileSet

	spelling *hunspell.Spell

	show          bool // show the context of a misspelling.
	ignoreUpper   bool // ignore words that are all uppercase.
	ignoreSingle  bool // ignore words that are a single rune.
	ignoreNumbers bool // ignore Go syntax number literals.
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

		if c.isCorrect(stripUnderscores(word), false) {
			continue
		}
		if !seen[word] {
			fmt.Printf("%v: %q is misspelled in %s", c.fileset.Position(pos), word, where)

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
	if isRetry {
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
		fragments = camel.Split(word)
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
		a.addWordUnknownWord(stripUnderscores(n.Name))
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
				a.addWordUnknownWord(w)
			}
		}
	}
	return a
}

func (a *adder) addWordUnknownWord(w string) {
	if a.spelling.IsCorrect(w) {
		return
	}
	ok := a.spelling.Add(w)
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

// isHex returns whether all bytes of s are hex digits.
func isHex(s string) bool {
	for _, b := range s {
		b |= 'a' - 'A' // Lower case in the relevant range.
		if (b < '0' || '9' < b) && (b < 'a' || 'f' < b) {
			return false
		}
	}
	return true
}

var (
	fset = token.NewFileSet()
	scan scanner.Scanner
)

// isNumber abuses the go/scanner to check whether word is a number.
func isNumber(word string) bool {
	var errored bool
	eh := func(_ token.Position, _ string) {
		errored = true
	}
	scan.Init(fset.AddFile("", fset.Base(), len(word)), []byte(word), eh, 0)
	_, tok, lit := scan.Scan()
	return !errored && lit == word && (tok == token.INT || tok == token.FLOAT || tok == token.IMAG)
}

// isHexRune returns whether word can be interpreted and a \x, \u, \U or
// \xxx octal rune literal.
func isHexRune(word string) bool {
	if len(word) < 4 || word[0] != '\\' {
		return false
	}
	switch word[1] {
	case 'x':
		return len(word) == 4 && isHex(word[2:4])
	case 'u':
		return len(word) == 6 && isHex(word[2:6])
	case 'U':
		return len(word) == 10 && isHex(word[2:10])
	default:
		if len(word) == 4 {
			return false
		}
		for _, c := range word[1:] {
			if c < '0' || '7' < c {
				return false
			}
		}
		return true
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
					return 3, false
				}
			}
			return 0, false
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
			spelling.Add(text[m[4]:m[5]])
		}
	}
}

// extractStructTagWords is derived from golang.org/x/tools/go/analysis/passes/structtag.
//
// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

var checkTagSpaces = map[string]bool{"json": true, "xml": true, "asn1": true}

// extractStructTagWords parses the struct tag and collects all the words
// in the struct tag. It returns nit if it is not in the canonical format,
// which is a space-separated list of key:"value" settings. The value may
// contain spaces.
func extractStructTagWords(tag string) []string {
	var kv []string

	// This code is based on the StructTag.Get code in package reflect.
	n := 0
	for ; tag != ""; n++ {
		if n > 0 && tag != "" && tag[0] != ' ' {
			// More restrictive than reflect, but catches likely mistakes
			// like `x:"foo",y:"bar"`, which parses as `x:"foo" ,y:"bar"` with second key ",y".
			return nil
		}
		// Skip leading space.
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		// Scan to colon. A space, a quote or a control character is a syntax error.
		// Strictly speaking, control chars include the range [0x7f, 0x9f], not just
		// [0x00, 0x1f], but in practice, we ignore the multi-byte control characters
		// as it is simpler to inspect the tag's bytes than the tag's runes.
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		if i == 0 {
			return nil
		}
		if i+1 >= len(tag) || tag[i] != ':' {
			return nil
		}
		if tag[i+1] != '"' {
			return nil
		}
		key := tag[:i]
		tag = tag[i+1:]

		// Get the struct tag key.
		kv = append(kv, key)

		// Scan quoted string to find value.
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			return nil
		}
		qvalue := tag[:i+1]
		tag = tag[i+1:]

		value, err := strconv.Unquote(qvalue)
		if err != nil {
			return nil
		}

		// Get all the struct tag values.
		kv = append(kv, strings.Split(value, ",")...)

		if !checkTagSpaces[key] {
			continue
		}

		switch key {
		case "xml":
			// If the first or last character in the XML tag is a space, it is
			// suspicious.
			if strings.Trim(value, " ") != value {
				return nil
			}

			// If there are multiple spaces, they are suspicious.
			if strings.Count(value, " ") > 1 {
				return nil
			}

			// If there is no comma, skip the rest of the checks.
			comma := strings.IndexRune(value, ',')
			if comma < 0 {
				continue
			}

			// If the character before a comma is a space, this is suspicious.
			if comma > 0 && value[comma-1] == ' ' {
				return nil
			}
			value = value[comma+1:]
		case "json":
			// JSON allows using spaces in the name, so skip it.
			comma := strings.IndexRune(value, ',')
			if comma < 0 {
				continue
			}
			value = value[comma+1:]
		}

		if strings.IndexByte(value, ' ') >= 0 {
			return nil
		}
	}

	return kv
}
