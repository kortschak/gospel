// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:generate go run gendoc.go path_linux.go config.go

// The gospel command finds and highlights misspelled words in Go source
// comments, strings and embedded files. It uses hunspell to identify
// misspellings and emits coloured output for visual inspection or error
// lists for use in automated linting.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/tools/go/packages"
)

func main() { os.Exit(gospel()) }

func gospel() (status int) {
	config, status, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return status
	}

	// Persisted options.
	flag.BoolVar(&config.IgnoreIdents, "ignore-idents", config.IgnoreIdents, "ignore words matching identifiers")
	flag.StringVar(&config.Lang, "lang", config.Lang, "language to use")
	flag.BoolVar(&config.Show, "show", config.Show, "print comment or string with misspellings")
	flag.BoolVar(&config.CheckStrings, "check-strings", config.CheckStrings, "check string literals")
	flag.BoolVar(&config.CheckEmbedded, "check-embedded", config.CheckEmbedded, "check embedded data files")
	flag.BoolVar(&config.IgnoreUpper, "ignore-upper", config.IgnoreUpper, "ignore all-uppercase words")
	flag.BoolVar(&config.IgnoreSingle, "ignore-single", config.IgnoreSingle, "ignore single letter words")
	flag.BoolVar(&config.IgnoreNumbers, "ignore-numbers", config.IgnoreNumbers, "ignore Go syntax number literals")
	flag.BoolVar(&config.MaskFlags, "mask-flags", config.MaskFlags, "ignore words with a leading dash")
	flag.BoolVar(&config.MaskURLs, "mask-urls", config.MaskURLs, "mask URLs in text")
	flag.BoolVar(&config.CheckURLs, "check-urls", config.CheckURLs, "check URLs in text with HEAD request")
	flag.BoolVar(&config.CamelSplit, "camel", config.CamelSplit, "split words on camel case")
	flag.BoolVar(&config.EntropyFiler.Filter, "entropy-filter", config.EntropyFiler.Filter, "filter strings by entropy")
	flag.IntVar(&config.MinNakedHex, "min-naked-hex", config.MinNakedHex, "length to recognize hex-digit words as number (0 is never ignore)")
	flag.IntVar(&config.MaxWordLen, "max-word-len", config.MaxWordLen, "ignore words longer than this (0 is no limit)")
	flag.IntVar(&config.MakeSuggestions, "suggest", config.MakeSuggestions, "make suggestions for misspellings (0 - never, 1 - first instance, 2 - always)")
	flag.IntVar(&config.DiffContext, "diff-context", config.DiffContext, "specify number of lines of change context to include")

	// Non-persisted config options.
	flag.StringVar(&config.paths, "dict-paths", config.paths, "directory list containing hunspell dictionaries")
	flag.StringVar(&config.words, "misspellings", "", "file to write a dictionary of misspellings (.dic format)")
	flag.BoolVar(&config.update, "update-dict", false, "update misspellings dictionary instead of creating a new one")
	flag.StringVar(&config.since, "since", config.since, "only consider changes since this ref (requires git)")

	writeConf := flag.Bool("write-config", false, "write config file based on flags and existing config to stdout and exit")
	flag.Bool("config", true, "use config file") // Included for documentation.
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

If a .gospel.conf file exists in the root of the current module and the config
flag is true (default) it will be used to populate selected flag defaults:
show, check-strings, ignore-upper, ignore-single, ignore-numbers, mask-urls,
camel, min-naked-hex, max-word-len and suggest.

String literals can be filtered on the basis of entropy to exclude unexpectedly
high or low complexity text from spell checking. This is experimental, and may
change in behaviour in future versions.

See https://github.com/kortschak/gospel for more complete documentation.

`, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if config.Lang == "" {
		fmt.Fprintln(os.Stderr, "missing lang flag")
		return invocationError
	}
	if config.MakeSuggestions < never || always < config.MakeSuggestions {
		fmt.Fprintln(os.Stderr, "invalid suggest flag value")
		return invocationError
	}
	if strings.Contains(config.since, "..") {
		fmt.Fprintln(os.Stderr, "cannot use commit range for since argument")
		return invocationError
	}

	if *writeConf {
		toml.NewEncoder(os.Stdout).Encode(config)
		return success
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

	d, err := newDictionary(pkgs, config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return internalError
	}

	c, err := newChecker(d, config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return invocationError
	}
	for _, p := range pkgs {
		c.fileset = p.Fset
		for _, f := range p.Syntax {
			if !c.changeFilter.fileIsInChange(f.Pos(), c.fileset) {
				continue
			}
			if c.CheckStrings {
				ast.Walk(c, f)
			}
			for _, g := range f.Comments {
				lastOK := true
				for i, l := range g.List {
					ok := c.check(l.Text, l, "comment")

					// Provide context for spelling in comments.
					if !ok {
						if i != 0 && lastOK {
							prev := g.List[i-1]
							c.misspellings = append(c.misspellings, misspelling{
								text: prev.Text,
								pos:  c.fileset.Position(prev.Pos()),
								end:  c.fileset.Position(prev.End()),
							})
						}
					} else {
						if !lastOK {
							c.misspellings = append(c.misspellings, misspelling{
								text: l.Text,
								pos:  c.fileset.Position(l.Pos()),
								end:  c.fileset.Position(l.End()),
							})
						}
					}
					lastOK = ok
				}
			}
		}
	}
	if c.CheckEmbedded {
		// TODO(kortschak): Remove this and use packages.Load
		// when https://go.dev/issue/50720 is resolved.
		embedded, err := embedFiles(flag.Args())
		if err != nil {
			fmt.Fprintf(os.Stdout, "could not get embedded files list: %v", err)
			return internalError
		}
		const maxLineLen = 120 // TODO(kortschak): Consider making this configurable.
		for _, path := range embedded {
			e, err := loadEmbedded(path, maxLineLen)
			if err != nil {
				fmt.Fprintf(os.Stdout, "could not read embedded file: %v", err)
				return internalError
			}
			if !c.changeFilter.fileIsInChange(e.Pos(), e) {
				continue
			}
			c.fileset = e
			c.check(e.Text(), e, "embedded file")
		}
	}
	if d.misspellings != 0 {
		status |= spellingError
	}
	c.report()

	err = d.writeMisspellings()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		status |= internalError
	}

	return status
}
