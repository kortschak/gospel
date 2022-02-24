// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
}

// newDictionary returns a new dictionary based on the provided packages
// and configuration.
func newDictionary(pkgs []*packages.Package, cfg config) (*dictionary, error) {
	d := dictionary{config: cfg}
	if d.words != "" {
		d.misspelled = make(map[string]bool)
	}

	var err error
	for _, p := range filepath.SplitList(d.paths) {
		if strings.HasPrefix(p, "~"+string(filepath.Separator)) {
			dir, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("could not expand tilde: %v", err)
			}
			p = filepath.Join(dir, p[2:])
		}
		d.Spell, err = hunspell.NewSpell(p, cfg.Lang)
		if err == nil {
			break
		}
	}
	if d.Spell == nil {
		return nil, fmt.Errorf("no %s dictionary found in: %v", d.Lang, d.paths)
	}

	// Load known words as a dictionary. This requires a write to
	// disk since hunspell does not allow dictionaries to be loaded
	// from memory, and affix rules can't be provided directly.
	kw, err := os.CreateTemp("", "gospel")
	if err != nil {
		return nil, fmt.Errorf("failed to create known words dictionary: %v", err)
	} else {
		defer os.Remove(kw.Name())
		fmt.Fprintln(kw, len(knownWords))
		for _, w := range knownWords {
			fmt.Fprintln(kw, w)
		}
		err := d.AddDict(kw.Name())
		if err != nil {
			return nil, err
		}
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
			err := d.AddDict(filepath.Join(r, ".words"))
			if _, ok := err.(*os.PathError); !ok && err != nil {
				return nil, err
			}
		}
	}

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
