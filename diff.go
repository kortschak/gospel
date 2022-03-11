// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/token"
	"io"
	"strconv"
	"strings"

	"golang.org/x/sys/execabs"
)

// changeFilter is a filter to exclude checks on words not in a set of
// code changes.
type changeFilter map[string][]lineRange

// isInChange returns whether pos is in changes in the filter. If f is nil
// all changes are included.
func (f changeFilter) isInChange(pos token.Pos, fset positioner) bool {
	if f == nil {
		return true
	}
	p := fset.Position(pos)
	lines, ok := f[rel(p.Filename)]
	if !ok {
		return false
	}
	for _, r := range lines {
		if r.start <= p.Line && p.Line <= r.end {
			return true
		}
	}
	return false
}

// fileIsInChange returns whether the file associated with pos is in
// changes in the filter. If f is nil all changes are included.
func (f changeFilter) fileIsInChange(pos token.Pos, fset positioner) bool {
	if f == nil {
		return true
	}
	_, ok := f[rel(fset.Position(pos).Filename)]
	return ok
}

// lineRange is a range of lines in a file, [start,end].
type lineRange struct{ start, end int }

// gitAdditionsSince returns a map of line additions in the current git
// repo since the specified ref. The context parameter specifies how
// many context lines are to be considered in an addition.
func gitAdditionsSince(ref string, context int) (changeFilter, error) {
	gitDiff := execabs.Command("git", "diff", fmt.Sprintf("-U%d", context), ref)
	var buf bytes.Buffer
	gitDiff.Stdout = &buf
	err := gitDiff.Run()
	if err != nil {
		return nil, err
	}
	return additions(&buf)
}

// additions returns a map of line additions calculated from unified diff
// data in r.
func additions(r io.Reader) (map[string][]lineRange, error) {
	const (
		fileAdditionPrefix = "+++ b/"
		hunkPrefix         = "@@ "
		deletionSuffix     = ",0"
	)

	additions := make(map[string][]lineRange)
	sc := bufio.NewScanner(r)
	var path string
	for sc.Scan() {
		switch {
		default:
			continue
		case bytes.HasPrefix(sc.Bytes(), []byte(fileAdditionPrefix)):
			path = strings.TrimPrefix(sc.Text(), fileAdditionPrefix)
		case bytes.HasPrefix(sc.Bytes(), []byte(hunkPrefix)):
			f := bytes.SplitN(sc.Bytes(), []byte{' '}, 4)
			if !bytes.HasPrefix(f[2], []byte{'+'}) {
				return nil, fmt.Errorf("malformed diff line: %s", sc.Bytes())
			}
			if bytes.HasSuffix(f[2], []byte(deletionSuffix)) {
				continue
			}
			hunk := string(f[2][1:])
			lines := 0
			var err error
			if idx := strings.Index(hunk, ","); idx >= 0 {
				lines, err = strconv.Atoi(hunk[idx+1:])
				if err != nil {
					return nil, fmt.Errorf("could not parse line range end: %w", err)
				}
				lines--
				hunk = hunk[:idx]
			}
			line, err := strconv.Atoi(hunk)
			if err != nil {
				return nil, fmt.Errorf("could not parse line range start: %w", err)
			}
			additions[path] = append(additions[path], lineRange{line, line + lines})
		}
	}
	return additions, nil
}
