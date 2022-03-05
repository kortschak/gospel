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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kortschak/hunspell"
	"golang.org/x/tools/go/packages"
)

// dictionary is a spelling dictionary that can record misspelled words.
type dictionary struct {
	*hunspell.Spell

	config

	// misspellings is the number of misspellings found.
	misspellings int

	// misspelled is the complete list of misspelled words
	// found during the check. The words must have had any
	// leading and trailing underscores removed.
	misspelled map[string]bool

	// roots is the set of module roots.
	roots map[string]bool

	// ignoredURLs is the set of URLs to omit from checking
	// target validity.
	ignoredURLs map[string]bool
}

// newDictionary returns a new dictionary based on the provided packages
// and configuration.
func newDictionary(pkgs []*packages.Package, cfg config) (*dictionary, error) {
	d := dictionary{config: cfg}
	if d.words != "" {
		d.misspelled = make(map[string]bool)
	}
	if d.CheckURLs {
		d.ignoredURLs = make(map[string]bool)
	}

	var (
		ook      librarian
		aff, dic string
		err      error
	)
	for _, p := range filepath.SplitList(d.paths) {
		if strings.HasPrefix(p, "~"+string(filepath.Separator)) {
			dir, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("could not expand tilde: %v", err)
			}
			p = filepath.Join(dir, p[2:])
		}
		aff, dic, err = hunspell.Paths(p, cfg.Lang)
		if err != nil {
			return nil, fmt.Errorf("could not find dictionary: %v", err)
		}
		ook, err = newLibrarian(aff, dic)
		if err == nil {
			for _, w := range knownWords {
				err = ook.addWord(w)
				if err != nil {
					return nil, fmt.Errorf("%w in internal dictionary", err)
				}
			}
			break
		}
	}
	if ook.rules == nil {
		return nil, fmt.Errorf("no %s dictionary found in: %v", d.Lang, d.paths)
	}

	// Load any dictionaries that exist in well known locations
	// at module roots. We do not do this when we are outputting
	// a misspelling list since the list will be incomplete unless
	// it is appended to the existing list, unless we are making
	// and updated dictionary when we will merge them.
	if d.words == "" || d.update {
		d.roots = make(map[string]bool)
		for _, p := range pkgs {
			if p.Module == nil {
				continue
			}
			d.roots[p.Module.Dir] = true
		}
		for r := range d.roots {
			err := ook.addDictionary(filepath.Join(r, ".words"))
			if _, ok := err.(*os.PathError); !ok && err != nil {
				return nil, err
			}
		}
	}

	// Load known words as a dictionary. This requires a write to
	// disk since hunspell does not allow dictionaries to be loaded
	// from memory, and affix rules can't be provided directly.
	kw, err := os.CreateTemp("", "gospel")
	if err != nil {
		return nil, fmt.Errorf("failed to create known words dictionary: %v", err)
	}
	defer func() {
		// In case we fail the write, close the file to allow
		// intransigent operating systems to delete it.
		kw.Close()
		os.Remove(kw.Name())
	}()
	err = ook.writeTo(kw)
	if err != nil {
		return nil, fmt.Errorf("failed to write known words dictionary: %v", err)
	}
	dic = kw.Name()
	err = kw.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to write known words dictionary: %v", err)
	}
	d.Spell, err = hunspell.NewSpellPaths(aff, dic)
	if err != nil {
		return nil, fmt.Errorf("could not open dictionary: %v", err)
	}

	// Get URLs if we are ignoring them.
	if d.CheckURLs {
		d.ignoredURLs = ook.urls
	}
	ook.urls = nil

	if cfg.IgnoreIdents {
		err = addIdentifiers(d.Spell, pkgs, make(map[string]bool))
		if err != nil {
			return nil, err
		}
	}

	// Add authors identifiers gleaned from NOTEs.
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			addNoteAuthors(d.Spell, f.Comments)
		}
	}

	return &d, nil
}

// noteMisspelling records the word as a misspelling if a words file was
// requested.
func (d *dictionary) noteMisspelling(word string) {
	d.misspellings++
	if d.misspelled != nil {
		d.misspelled[word] = true
	}
}

// writeMisspellings writes the recorded misspellings to the words file.
func (d *dictionary) writeMisspellings() error {
	// Write out a dictionary of the misspelled words.
	// The hunspell .dic format includes a count hint
	// at the top of the file so add that as well.
	if d.words != "" {
		if d.update {
			// Carry over words from the already existing dictionaries.
			for r := range d.roots {
				old, err := os.Open(filepath.Join(r, ".words"))
				if err == nil {
					sc := bufio.NewScanner(old)
					for i := 0; sc.Scan(); i++ {
						if i == 0 {
							continue
						}
						d.misspelled[sc.Text()] = true
					}
					old.Close()
				} else if !errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("failed to open .words file: %v", err)
				}
			}
		}

		f, err := os.Create(d.words)
		if err != nil {
			return fmt.Errorf("failed to open misspellings file: %v", err)
		}
		defer f.Close()
		dict := make([]string, 0, len(d.misspelled))
		for m := range d.misspelled {
			dict = append(dict, m)
		}
		sort.Strings(dict)
		_, err = fmt.Fprintln(f, len(dict))
		if err != nil {
			return fmt.Errorf("failed to write new dictionary: %v", err)
		}
		for _, m := range dict {
			_, err = fmt.Fprintln(f, m)
			if err != nil {
				return fmt.Errorf("failed to write new dictionary: %v", err)
			}
		}
	}

	return nil
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

// a librarian collates dictionaries.
type librarian struct {
	rules map[string]string
	urls  map[string]bool
}

// newLibrarian returns a new librarian populated with words and affix rules
// obtained from the hunspell .dic file paths provided, checking that the
// affix file aff also exists.
func newLibrarian(aff, dic string) (librarian, error) {
	_, err := os.Stat(aff)
	if err != nil {
		return librarian{}, err
	}
	l := librarian{
		rules: make(map[string]string),
		urls:  make(map[string]bool),
	}
	err = l.addDictionary(dic)
	if err != nil {
		return librarian{}, err
	}
	return l, nil
}

// addDictionary adds word rules from the hunspell dictionary at the given
// path.
func (l librarian) addDictionary(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan(); i++ {
		if i == 0 {
			// Skip word count line.
			continue
		}
		err := l.addWord(sc.Text())
		if err != nil {
			return fmt.Errorf("%w at %s:%d", err, path, i+1)
		}
	}
	return sc.Err()
}

// addWord adds the provided word to the librarian's dictionary merging any
// affix rules into those already existing for the word.
func (l librarian) addWord(w string) error {
	r := strings.Split(w, "/")
	word := r[0]
	if word == "" {
		// This should never happen, but we can ignore it.
		return nil
	}
	var affix string
	switch len(r) {
	case 1:
	case 2:
		affix = r[1]
	default:
		if urls.MatchString(w) {
			l.urls[w] = true
			return nil
		}
		return fmt.Errorf("invalid dictionary entry %q", w)
	}
	l.rules[word] = mergeRules(l.rules[word], affix)
	return nil
}

// mergeRules merges affix rules.
func mergeRules(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		r := make([]rune, 0, utf8.RuneCountInString(a)+utf8.RuneCountInString(b))
		r = append(r, []rune(a)...)
		r = append(r, []rune(b)...)
		sort.Slice(r, func(i, j int) bool { return r[i] < r[j] })
		curr := 0
		for i, e := range r {
			if e == r[curr] {
				continue
			}
			curr++
			if curr < i {
				r[curr], r[i] = r[i], 0
			}
		}
		return string(r[:curr+1])
	}
}

// writeTo writes the word rules in the librarian to the provided io.Writer
// in hunspell .dic format.
func (l librarian) writeTo(w io.Writer) error {
	dict := make([]string, 0, len(l.rules))
	for w, r := range l.rules {
		if r != "" {
			dict = append(dict, w+"/"+r)
		} else {
			dict = append(dict, w)
		}
	}
	_, err := fmt.Fprintln(w, len(dict))
	if err != nil {
		return fmt.Errorf("failed to write new dictionary: %v", err)
	}
	// We don't sort here since it's for immediate consumption by hunspell.
	for _, r := range dict {
		_, err = fmt.Fprintln(w, r)
		if err != nil {
			return fmt.Errorf("failed to write new dictionary: %v", err)
		}
	}
	return nil
}
