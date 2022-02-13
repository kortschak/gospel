// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"strconv"
	"strings"
)

// extractStructTagWords is derived from golang.org/x/tools/go/analysis/passes/structtag.
//
// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

var checkTagSpaces = map[string]bool{"json": true, "xml": true, "asn1": true}

// extractStructTagWords parses the struct tag and collects all the words
// in the struct tag. It returns nit if it is not in the canonical format,
// which is a space-separated list of key:"value" settings. The value may
// contain spaces.
func extractStructTagWords(tag string) []string {
	var kv []string

	// This code is based on the StructTag.Get code in package reflect.
	n := 0
	for ; tag != ""; n++ {
		if n > 0 && tag != "" && tag[0] != ' ' {
			// More restrictive than reflect, but catches likely mistakes
			// like `x:"foo",y:"bar"`, which parses as `x:"foo" ,y:"bar"` with second key ",y".
			return nil
		}
		// Skip leading space.
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		// Scan to colon. A space, a quote or a control character is a syntax error.
		// Strictly speaking, control chars include the range [0x7f, 0x9f], not just
		// [0x00, 0x1f], but in practice, we ignore the multi-byte control characters
		// as it is simpler to inspect the tag's bytes than the tag's runes.
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		if i == 0 {
			return nil
		}
		if i+1 >= len(tag) || tag[i] != ':' {
			return nil
		}
		if tag[i+1] != '"' {
			return nil
		}
		key := tag[:i]
		tag = tag[i+1:]

		// Get the struct tag key.
		kv = append(kv, key)

		// Scan quoted string to find value.
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			return nil
		}
		qvalue := tag[:i+1]
		tag = tag[i+1:]

		value, err := strconv.Unquote(qvalue)
		if err != nil {
			return nil
		}

		// Get all the struct tag values.
		kv = append(kv, strings.Split(value, ",")...)

		if !checkTagSpaces[key] {
			continue
		}

		switch key {
		case "xml":
			// If the first or last character in the XML tag is a space, it is
			// suspicious.
			if strings.Trim(value, " ") != value {
				return nil
			}

			// If there are multiple spaces, they are suspicious.
			if strings.Count(value, " ") > 1 {
				return nil
			}

			// If there is no comma, skip the rest of the checks.
			comma := strings.IndexRune(value, ',')
			if comma < 0 {
				continue
			}

			// If the character before a comma is a space, this is suspicious.
			if comma > 0 && value[comma-1] == ' ' {
				return nil
			}
			value = value[comma+1:]
		case "json":
			// JSON allows using spaces in the name, so skip it.
			comma := strings.IndexRune(value, ',')
			if comma < 0 {
				continue
			}
			value = value[comma+1:]
		}

		if strings.IndexByte(value, ' ') >= 0 {
			return nil
		}
	}

	return kv
}
