// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"

	"github.com/kortschak/hunspell"
	"golang.org/x/sys/execabs"
)

// readGitLog adds author names and email addresses from git log.
func readGitLog(spelling *hunspell.Spell) {
	cmd := execabs.Command("git", "log", "--format=%an %ae")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	err := cmd.Run()
	if err != nil {
		return
	}
	sc := bufio.NewScanner(&buf)
	var w words // Use our word scanner to retain parity.
	sc.Split(w.ScanWords)
	for sc.Scan() {
		w := sc.Text()
		if spelling.IsCorrect(w) {
			continue
		}
		spelling.Add(w)
	}
}
