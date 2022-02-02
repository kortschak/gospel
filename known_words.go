// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// knownWords contains a list of commonly encountered words that
// may not be in user dictionaries.
var knownWords = []string{
	"golang",

	// Keywords
	"break", "case", "chan", "const", "continue", "default",
	"defer", "else", "fallthrough", "for", "func", "go", "goto",
	"if", "import", "interface", "map", "package", "range",
	"return", "select", "struct", "switch", "type", "var",

	// Built-in
	"append", "cap", "cgo", "copy", "goroutine", "goroutines", "init",
	"inits", "len", "make", "map", "new", "nil", "panic", "print",
	"println", "recover",

	// Built-in types
	"bool",
	"int", "int8", "int16", "int32", "int64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"float32", "float64",
	"complex64", "complex128",
	"string", "byte", "rune",

	// Commonly used words
	"allocator", "args", "async", "boolean", "booleans", "codec", "endian",
	"gcc", "hostname", "http", "https", "localhost", "NaN", "NaNs", "rpc",
	"symlink", "symlinks", "toolchain", "toolchains",

	// Architectures
	"aix", "amd64", "arm64", "darwin", "freebsd", "illumos", "ios", "js",
	"linux", "mips", "mips64", "mips64le", "mipsle", "netbsd", "openbsd",
	"ppc64", "ppc64le", "riscv64", "s390x", "solaris", "wasm", "windows",

	// Compiler comments
	"c1",
	"c2",
	"cgo_dynamic_linker",
	"cgo_export_dynamic",
	"cgo_export_static",
	"cgo_import_dynamic",
	"cgo_import_static",
	"cgo_ldflag",
	"cgo_unsafe_args",
	"d1",
	"d2",
	"e1",
	"e2",
	"empty1",
	"empty2",
	"linkname",
	"nocheckptr",
	"noescape",
	"noinline",
	"nointerface",
	"norace",
	"nosplit",
	"notinheap",
	"nowritebarrier",
	"nowritebarrierrec",
	"registerparams",
	"systemstack",
	"uintptrescapes",
	"yeswritebarrierrec",

	// Common hosters
	"bitbucket", "github", "gitlab", "sourcehut", "sr", "ht",
}
