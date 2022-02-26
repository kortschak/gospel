// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The gospel command finds and highlights misspelled words in Go source
// comments and strings. It uses hunspell to identify misspellings and only
// emits coloured output for visual inspection; don't use it in automated
// linting.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/tools/go/packages"
)

func main() { os.Exit(gospel()) }

// Exit status codes.
const (
	success       = 0
	internalError = 1 << (iota - 1)
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
	IgnoreIdents    bool          `toml:"ignore_idents"`  // ignore words matching identifiers.
	Lang            string        `toml:"lang"`           // language to use.
	Show            bool          `toml:"show"`           // show the context of a misspelling.
	CheckStrings    bool          `toml:"check_strings"`  // check string literals as well as comments.
	IgnoreUpper     bool          `toml:"ignore_upper"`   // ignore words that are all uppercase.
	IgnoreSingle    bool          `toml:"ignore_single"`  // ignore words that are a single rune.
	IgnoreNumbers   bool          `toml:"ignore_numbers"` // ignore Go syntax number literals.
	MaskURLs        bool          `toml:"mask_urls"`      // mask URLs before checking.
	CamelSplit      bool          `toml:"camel"`          // split words on camelCase when retrying.
	MaxWordLen      int           `toml:"max_word_len"`   // ignore words longer than this.
	MinNakedHex     int           `toml:"min_naked_hex"`  // ignore words at least this long if only hex digits.
	Patterns        []string      `toml:"patterns"`       // acceptable words defined by regexp.
	MakeSuggestions int           `toml:"suggest"`        // make suggestions for misspelled words.
	EntropyFiler    entropyFilter `toml:"entropy_filter"` // specify entropy filter behaviour (experimental).

	words  string
	paths  string
	update bool
}

var defaults = config{
	// Dictionary options.
	IgnoreIdents: true,
	Lang:         "en_US",

	paths: path,

	// Checker options.
	Show:            true,
	CheckStrings:    false,
	IgnoreUpper:     true,
	IgnoreSingle:    true,
	IgnoreNumbers:   true,
	MaskURLs:        true,
	CamelSplit:      true,
	MaxWordLen:      40,
	MinNakedHex:     8,
	MakeSuggestions: never,

	// Experimental options.
	EntropyFiler: entropyFilter{
		Filter:         false,
		MinLenFiltered: 16,
		Accept:         intRange{Low: 14, High: 20},
	},
}

// entropyFilter specifies behaviour of the entropy filter.
type entropyFilter struct {
	Filter bool `toml:"filter"`

	// MinLenFiltered is the shorted text
	// length that will be considered by
	// the entropy filter.
	MinLenFiltered int `toml:"min_len_filtered"`

	// Accept is the range of effective
	// alphabet sizes that are acceptable.
	Accept intRange `toml:"accept"`
}

// intRange is an int interval.
type intRange struct {
	Low  int `toml:"low"`
	High int `toml:"high"`
}

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
	flag.BoolVar(&config.IgnoreUpper, "ignore-upper", config.IgnoreUpper, "ignore all-uppercase words")
	flag.BoolVar(&config.IgnoreSingle, "ignore-single", config.IgnoreSingle, "ignore single letter words")
	flag.BoolVar(&config.IgnoreNumbers, "ignore-numbers", config.IgnoreNumbers, "ignore Go syntax number literals")
	flag.BoolVar(&config.MaskURLs, "mask-urls", config.MaskURLs, "mask URLs in text")
	flag.BoolVar(&config.CamelSplit, "camel", config.CamelSplit, "split words on camel case")
	flag.BoolVar(&config.EntropyFiler.Filter, "entropy-filter", config.EntropyFiler.Filter, "filter strings by entropy")
	flag.IntVar(&config.MinNakedHex, "min-naked-hex", config.MinNakedHex, "length to recognize hex-digit words as number (0 is never ignore)")
	flag.IntVar(&config.MaxWordLen, "max-word-len", config.MaxWordLen, "ignore words longer than this (0 is no limit)")
	flag.IntVar(&config.MakeSuggestions, "suggest", config.MakeSuggestions, "make suggestions for misspellings (0 - never, 1 - first instance, 2 - always)")

	// Non-persisted config options.
	flag.StringVar(&config.paths, "dict-paths", config.paths, "directory list containing hunspell dictionaries")
	flag.StringVar(&config.words, "misspellings", "", "file to write a dictionary of misspellings (.dic format)")
	flag.BoolVar(&config.update, "update-dict", false, "update misspellings dictionary instead of creating a new one")

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

const configFile = ".gospel.conf"

// loadConfig returns a config if one can be found in the root of the
// current module. It also returns a status and error for user information.
func loadConfig() (_ config, status int, err error) {
	// Using to the flag package to get this information early results
	// in horrific convolutions, and while it works, it is sludgy. So
	// do the work ourselves.
	useConfig := true // Default to true.
	args := os.Args[1:]
loop:
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") {
			arg = arg[1:]
		}
		if !strings.HasPrefix(arg, "-config") {
			continue
		}
		val := strings.TrimPrefix(arg, "-config")
		switch val {
		case "", "=true":
			useConfig = true
			break loop
		case "=false":
			useConfig = false
			break loop
		default:
			// Let command-line flag parser handle this.
			return config{}, success, nil
		}
	}
	if !useConfig {
		return defaults, success, nil
	}

	cfg := &packages.Config{Mode: packages.NeedModule}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		// Can't find module, but we may have been asked for other
		// things, so if there are errors, let the actual package
		// loader find then.
		return defaults, success, nil
	}
	mod := pkgs[0].Module
	if mod == nil {
		return defaults, success, nil
	}

	_, err = toml.DecodeFile(filepath.Join(mod.Dir, configFile), &defaults)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return defaults, success, nil
		}
		return config{}, invocationError, err
	}
	return defaults, success, nil
}
