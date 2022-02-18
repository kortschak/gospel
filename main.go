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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// config holds application-wide user configuration values.
type config struct {
	show            bool // show the context of a misspelling.
	checkStrings    bool // check string literals as well as comments.
	ignoreUpper     bool // ignore words that are all uppercase.
	ignoreSingle    bool // ignore words that are a single rune.
	ignoreNumbers   bool // ignore Go syntax number literals.
	maskURLs        bool // mask URLs before checking.
	camelSplit      bool // split words on camelCase when retrying.
	maxWordLen      int  // ignore words longer than this.
	minNakedHex     int  // ignore words at least this long if only hex digits.
	makeSuggestions int  // make suggestions for misspelled words.
}

func gospel() (status int) {
	show := flag.Bool("show", true, "print comment or string with misspellings")
	checkStrings := flag.Bool("check-strings", false, "check string literals")
	ignoreUpper := flag.Bool("ignore-upper", true, "ignore all-uppercase words")
	ignoreSingle := flag.Bool("ignore-single", true, "ignore single letter words")
	ignoreIdents := flag.Bool("ignore-idents", true, "ignore words matching identifiers")
	ignoreNumbers := flag.Bool("ignore-numbers", true, "ignore Go syntax number literals")
	maskURLs := flag.Bool("mask-urls", true, "mask URLs in text")
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
	var roots map[string]bool
	if *words == "" || *update {
		roots = make(map[string]bool)
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

	keep := *words != ""
	c := newChecker(spelling, keep, config{
		show:            *show,
		checkStrings:    *checkStrings,
		ignoreUpper:     *ignoreUpper,
		ignoreSingle:    *ignoreSingle,
		ignoreNumbers:   *ignoreNumbers,
		maskURLs:        *maskURLs,
		camelSplit:      *camelSplit,
		maxWordLen:      *maxWordLen,
		minNakedHex:     *minNakedHex,
		makeSuggestions: *suggest,
	})

	for _, p := range pkgs {
		c.fileset = p.Fset
		for _, f := range p.Syntax {
			if c.checkStrings {
				ast.Walk(c, f)
			}
			for _, g := range f.Comments {
				// TODO(kortschak): Check each line of comment
				// individually and provide ±line of context.
				// This reduces output and takes the user to
				// the error location more quickly. It also
				// means that we can spell check reasons in
				// linting and compiler directives.
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
	if keep {
		if *update {
			// Carry over words from the already existing dictionaries.
			for r := range roots {
				old, err := os.Open(filepath.Join(r, ".words"))
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
