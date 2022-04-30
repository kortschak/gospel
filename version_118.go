// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.18
// +build go1.18

package main

import (
	"fmt"
	"runtime/debug"
)

func buildSettings(info *debug.BuildInfo) {
	fmt.Printf("Build settings:\n")
	for _, setting := range info.Settings {
		if setting.Value == "" {
			continue // do empty build settings even matter?
		}
		// The padding helps keep readability by aligning:
		//
		//   veryverylong.key value
		//          short.key some-other-value
		//
		// Empirically, 16 is enough; the longest key seen is "vcs.revision".
		fmt.Printf("%16s %s\n", setting.Key, setting.Value)
	}
}
