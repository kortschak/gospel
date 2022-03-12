// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/google/licensecheck"
	"github.com/kortschak/hunspell"
)

// readLicenses adds words from licenses under root that satisfy the licensecheck
// threshold provided.
func readLicenses(spelling *hunspell.Spell, root string, thresh float64) error {
	texts, err := licenses(root, thresh)
	if err != nil {
		return err
	}
	for _, text := range texts {
		sc := bufio.NewScanner(strings.NewReader(text))
		var w words // Use our word scanner to retain parity.
		sc.Split(w.ScanWords)
		for sc.Scan() {
			w := quietly(sc.Text())
			if spelling.IsCorrect(w) {
				continue
			}
			spelling.Add(w)
		}
	}
	return nil
}

// quietly returns the provided string lower cased if it is all upper case.
func quietly(s string) string {
	for _, r := range s {
		if !unicode.IsUpper(r) {
			return s
		}
	}
	return strings.ToLower(s)
}

// licenses returns the text of all files matching licenses using
// licensecheck.Scan with at least a thresh match.
func licenses(root string, thresh float64) ([]string, error) {
	maybeLicense := make(map[string]bool)
	for _, c := range candidates {
		maybeLicense[strings.ToLower(c)] = true
	}

	var texts []string
	err := filepath.WalkDir(root, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
		if !maybeLicense[strings.ToLower(name)] {
			return nil
		}
		b, err := os.ReadFile(info.Name())
		if err != nil {
			return err
		}
		if licensecheck.Scan(b).Percent >= thresh {
			texts = append(texts, string(b))
		}
		return nil
	})
	return texts, err
}

var candidates = []string{
	"COPYING",
	"LICENCE",
	"LICENSE",
	"LICENSE-2.0",
	"LICENCE-2.0",
	"LICENSE-APACHE",
	"LICENCE-APACHE",
	"LICENSE-APACHE-2.0",
	"LICENCE-APACHE-2.0",
	"LICENSE-MIT",
	"LICENCE-MIT",
	"MIT-LICENSE",
	"MIT-LICENCE",
	"MIT_LICENSE",
	"MIT_LICENCE",
	"UNLICENSE",
	"UNLICENCE",
}
