// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/tools/go/packages"
)

// Exit status codes.
const (
	success       = 0
	internalError = 1 << (iota - 1)
	invocationError
	directiveError // Currently unused. This will be for linting directives.
	spellingError
)

// config holds application-wide user configuration values.
type config struct {
	IgnoreIdents    bool          `toml:"ignore_idents"`  // ignore words matching identifiers.
	Lang            string        `toml:"lang"`           // language to use.
	Show            bool          `toml:"show"`           // show the context of a misspelling.
	CheckStrings    bool          `toml:"check_strings"`  // check string literals as well as comments.
	CheckEmbedded   bool          `toml:"check_embedded"` // check spelling in embedded files as well as comments.
	IgnoreUpper     bool          `toml:"ignore_upper"`   // ignore words that are all uppercase.
	IgnoreSingle    bool          `toml:"ignore_single"`  // ignore words that are a single rune.
	IgnoreNumbers   bool          `toml:"ignore_numbers"` // ignore Go syntax number literals.
	ReadLicenses    bool          `toml:"read_licenses"`  // ignore all words found in license files.
	GitLog          bool          `toml:"read_git_log"`   // ignore all author names and emails found in git log.
	MaskFlags       bool          `toml:"mask_flags"`     // ignore words with a leading dash.
	MaskURLs        bool          `toml:"mask_urls"`      // mask URLs before checking.
	CheckURLs       bool          `toml:"check_urls"`     // check URLs point to reachable targets.
	CamelSplit      bool          `toml:"camel"`          // split words on camelCase when retrying.
	MaxWordLen      int           `toml:"max_word_len"`   // ignore words longer than this.
	MinNakedHex     int           `toml:"min_naked_hex"`  // ignore words at least this long if only hex digits.
	Patterns        []string      `toml:"patterns"`       // acceptable words defined by regexp.
	MakeSuggestions suggest       `toml:"suggest"`        // make suggestions for misspelled words.
	DiffContext     int           `toml:"diff_context"`   // specify number of lines of change context to include.
	EntropyFiler    entropyFilter `toml:"entropy_filter"` // specify entropy filter behaviour (experimental).

	since  string
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
	CheckEmbedded:   false,
	IgnoreUpper:     true,
	IgnoreSingle:    true,
	IgnoreNumbers:   true,
	ReadLicenses:    true,
	GitLog:          true,
	MaskFlags:       false,
	MaskURLs:        true,
	CheckURLs:       false,
	CamelSplit:      true,
	MaxWordLen:      40,
	MinNakedHex:     8,
	MakeSuggestions: never,
	DiffContext:     0,

	// Experimental options.
	EntropyFiler: entropyFilter{
		Filter:         false,
		MinLenFiltered: 16,
		Accept:         intRange{Low: 14, High: 20},
	},
}

// Suggestion behaviour.
//go:generate stringer -type=suggest
const (
	never suggest = iota
	once
	each
	always
)

type suggest int

func (s suggest) MarshalText() ([]byte, error)  { return []byte(s.String()), nil }
func (s *suggest) UnmarshalText(b []byte) error { return s.Set(string(b)) }

func (s *suggest) Set(val string) error {
	for i := never; i <= always; i++ {
		if val == i.String() {
			*s = i
			return nil
		}
	}
	return fmt.Errorf(`valid options are "never", "once", "each" and "always"`)
}

// entropyFilter specifies behaviour of the entropy filter.
type entropyFilter struct {
	Filter bool `toml:"filter"`

	// MinLenFiltered is the shortest text
	// length that will be considered by
	// the entropy filter.
	MinLenFiltered int `toml:"min_len_filtered"`

	// Accept is the range of effective
	// alphabet sizes that are acceptable
	// as text that may contain words
	// needing spell checking.
	Accept intRange `toml:"accept"`
}

// intRange is an int interval.
type intRange struct {
	Low  int `toml:"low"`
	High int `toml:"high"`
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
