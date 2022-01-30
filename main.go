// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The gospel command finds and highlights misspelled words in Go source
// comments. It uses hunspell to identify misspellings and only emits
// coloured output for visual inspection; don't use it in automated linting.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kortschak/ct"
	"github.com/kortschak/hunspell"
	"golang.org/x/tools/go/packages"
)

func main() {
	show := flag.Bool("show", true, "print comment with misspellings")
	ignoreUpper := flag.Bool("ignore-upper", true, "ignore all-uppercase words")
	ignoreIdents := flag.Bool("ignore-idents", true, "ignore words matching identifiers")
	lang := flag.String("lang", "en_US", "language to use")
	dicts := flag.String("dict-paths", path, "directory list containing hunspell dictionaries")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `usage: %s [options] [packages]

The gospel program will report misspellings in Go source comments.

Each comment block with misspelled word will be output for each word and
if the -show flag is true, the complete comment block will be printed with
misspelled words highlighted.

`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *lang == "" {
		fmt.Fprintf(os.Stderr, "missing lang flag")
		os.Exit(1)
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
				os.Exit(1)
			}
			p = filepath.Join(dir, p[2:])
		}
		spelling, err = hunspell.NewSpell(p, *lang)
		if err == nil {
			break
		}
	}
	if spelling == nil {
		fmt.Fprintf(os.Stderr, "no dictionaries found in: %v\n", *dicts)
		os.Exit(1)
	}
	for _, w := range knownWords {
		spelling.Add(w)
	}

	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedDeps,
	}
	pkgs, err := packages.Load(cfg, flag.Args()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}
	if packages.PrintErrors(pkgs) != 0 {
		os.Exit(1)
	}

	if *ignoreIdents {
		err = addIdentifiers(spelling, pkgs)
		if err != nil {
			log.Fatal(err)
		}
	}

	warn := (ct.Italic | ct.Fg(ct.BoldRed)).Paint

	for _, p := range pkgs {
		for _, f := range p.Syntax {
			for _, c := range f.Comments {
				comment := c.Text()
				sc := bufio.NewScanner(strings.NewReader(comment))
				w := words{}
				sc.Split(w.ScanWords)

				var (
					args    []interface{}
					lastPos int
				)
				seen := make(map[string]bool)
				for sc.Scan() {
					text := sc.Text()
					switch {
					case strings.HasSuffix(text, "'s"):
						text = strings.TrimSuffix(text, "'s")
					case strings.HasSuffix(text, "'d"):
						text = strings.TrimSuffix(text, "'d")
					case strings.HasSuffix(text, "'th"):
						text = strings.TrimSuffix(text, "'th")
					}

					if !(*ignoreUpper && allUpper(text)) && !spelling.IsCorrect(text) {
						if !seen[text] {
							fmt.Printf("%v: %q is misspelled\n", p.Fset.Position(c.Pos()), text)
							seen[text] = true
						}
						if *show {
							if w.current.pos != lastPos {
								args = append(args, comment[lastPos:w.current.pos])
							}
							args = append(args, warn(comment[w.current.pos:w.current.pos+len(text)]), comment[w.current.pos+len(text):w.current.end])
							lastPos = w.current.end
						}
					}
				}
				if args != nil {
					if lastPos != len(comment) {
						args = append(args, comment[lastPos:])
					}
					lines := strings.Split(join(args), "\n")
					if lines[len(lines)-1] == "" {
						lines = lines[:len(lines)-1]
					}
					fmt.Printf("\t%s\n", strings.Join(lines, "\n\t"))
				}
			}
		}
	}
}

// addIdentifiers adds identifier labels to the spelling dictionary.
func addIdentifiers(spelling *hunspell.Spell, pkgs []*packages.Package) error {
	v := &visitor{spelling: spelling}
	for _, p := range pkgs {
		for _, e := range strings.Split(p.String(), "/") {
			spelling.Add(e)
		}
		for _, f := range p.Syntax {
			ast.Walk(v, f)
		}
	}
	if v.failed != 0 {
		return errors.New("missed adding %d identifiers")
	}
	return nil
}

type visitor struct {
	spelling *hunspell.Spell
	failed   int
}

func (v *visitor) Visit(n ast.Node) ast.Visitor {
	if n, ok := n.(*ast.Ident); ok {
		ok = v.spelling.Add(n.Name)
		if !ok {
			v.failed++
		}
	}
	return v
}

// allUpper returns whether all runes in s are uppercase. For the purposed
// of this test, numerals are considered uppercase.
func allUpper(s string) bool {
	for _, r := range s {
		if !unicode.IsUpper(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
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
		if !unicode.IsSpace(r) && !unicode.IsSymbol(r) && !isWordSplitPunct(prev, r, data[i+width:]) {
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
		if unicode.IsSpace(r) || unicode.IsSymbol(r) || isWordSplitPunct(prev, r, data[i+width:]) {
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

// isWordSplitPunct returns whether the previous, current and next runes
// indicate that the current rune splits words.
func isWordSplitPunct(prev, curr rune, next []byte) bool {
	return curr != '_' && unicode.IsPunct(curr) && !isApostrophe(prev, curr, next)
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

var knownWords = []string{
	"golang",

	"break", "case", "chan", "const", "continue", "default",
	"defer", "else", "fallthrough", "for", "func", "go", "goto",
	"if", "import", "interface", "map", "package", "range",
	"return", "select", "struct", "switch", "type", "var",

	"append", "cap", "cgo", "copy", "goroutine", "goroutines", "init",
	"len", "make", "map", "new", "panic", "print", "println", "recover",

	"allocator", "args", "async", "boolean", "booleans", "codec", "endian",
	"gcc", "hostname", "http", "https", "localhost", "rpc", "symlink",
	"symlinks",

	"aix", "amd64", "arm64", "darwin", "freebsd", "illumos", "ios", "js",
	"linux", "mips", "mips64", "mips64le", "mipsle", "netbsd", "openbsd",
	"ppc64", "ppc64le", "riscv64", "s390x", "solaris", "wasm", "windows",

	"linkname", "nosplit", "toolchain",
}
