// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !go1.18
// +build !go1.18

package main

import (
	"fmt"
	"runtime/debug"
)

func buildSettings(_ *debug.BuildInfo) {
	fmt.Printf("Build settings: not available\n")
}
