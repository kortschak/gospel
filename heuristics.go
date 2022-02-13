// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"go/scanner"
	"go/token"
	"strings"
	"unicode"
)

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
