// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// heuristic is a type that can give suggest whether a word is acceptable.
type heuristic interface {
	// isAcceptable returns whether the provided word is acceptable. If
	// partial is true, the word is a portion of a whole word that has
	// been split.
	isAcceptable(word string, partial bool) bool
}

// wordLen is a word length heuristic.
type wordLen struct {
	max int
}

// isAcceptable returns whether the query word is over the maximum word
// length to consider.
func (h wordLen) isAcceptable(word string, _ bool) bool {
	return h.max > 0 && len(word) > h.max
}

// allUpper is a heuristic that accepts all-uppercase words.
type allUpper struct{}

// isAcceptable returns whether all runes in word are uppercase. For the
// purposes of this test, numerals and underscores are considered uppercase.
// As a special case, a final 's' is also considered uppercase to allow
// plurals of initialisms and acronyms.
func (allUpper) isAcceptable(word string, _ bool) bool {
	word = strings.TrimSuffix(word, "s")
	for _, r := range word {
		if !unicode.IsUpper(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// isSingle is a heuristic that accepts single-rune words.
type isSingle struct{}

// isAcceptable returns whether the query word is a single rune.
func (isSingle) isAcceptable(word string, _ bool) bool {
	return utf8.RuneCountInString(word) == 1
}

// isNakedHex is a heuristic that accepts hex numbers as valid words
type isNakedHex struct {
	// minLen is a minimum length that will be accepted. This
	// prevents accidental acceptance of short misspelled words
	// with only hex digits.
	minLen int
}

// isAcceptable returns whether the query word is a hex number.
func (h isNakedHex) isAcceptable(word string, _ bool) bool {
	return h.minLen != 0 && len(word) >= h.minLen && isHex(word)
}

// isNumber is a heuristic that accepts all Go syntax numbers as
// valid words.
type isNumber struct {
	scanner scanner.Scanner
}

// isAcceptable abuses the go/scanner to check whether word is a number.
func (h *isNumber) isAcceptable(word string, _ bool) bool {
	var errored bool
	eh := func(_ token.Position, _ string) {
		errored = true
	}
	fset := token.NewFileSet()
	h.scanner.Init(fset.AddFile("", fset.Base(), len(word)), []byte(word), eh, 0)
	_, tok, lit := h.scanner.Scan()
	return !errored && lit == word && (tok == token.INT || tok == token.FLOAT || tok == token.IMAG)
}

// isHexRune is a heuristic that accepts Go rune literal syntax as a valid
// word.
type isHexRune struct{}

// isAcceptable returns whether word can be interpreted and a \x, \u, \U or
// \xxx octal rune literal.
func (isHexRune) isAcceptable(word string, _ bool) bool {
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

// isUnit is a heuristic that accepts quantities with units as valid words.
type isUnit struct{}

// isUnit returns whether word is a quantity with a unit. Naked units are
// handled by hunspell. If partial is true, word is not a valid unit as
// it would have been directly adjacent to other characters.
func (isUnit) isAcceptable(word string, partial bool) bool {
	if partial {
		// Don't consider camel split words for unit heuristic.
		return false
	}
	for _, u := range knownUnits {
		if strings.HasSuffix(word, u) {
			_, err := strconv.ParseFloat(strings.TrimSuffix(word, u), 64)
			if err == nil {
				// We have to check all of them until we get an
				// acceptance unless we guarantee that no suffix
				// of a unit exists that is also a unit later in
				// the list. If performance becomes an issue do
				// this.
				return true
			}
		}
	}
	return false
}

// knownUnits is the set of units we check for. Add more as they are
// identified as problems.
var knownUnits = []string{
	"k", "M", "x",
	"Kb", "kb", "Mb", "Gb", "Tb",
	"KB", "kB", "MB", "GB", "TB",
	"Kib", "kib", "Mib", "Gib", "Tib",
	"KiB", "kiB", "MiB", "GiB", "TiB",
	"Å", "nm", "µm", "mm", "cm", "m", "km",
	"ns", "µs", "us", "ms", "s", "min", "hr",
	"Hz",
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
