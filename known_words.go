// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// knownWords contains a list of commonly encountered words that
// may not be in user dictionaries. It is used to construct a
// temporary dictionary to load into hunspell.
var knownWords = []string{
	"golang",

	// Keywords
	"break/BMZGRS", "case/LDSJMG", "chan/MS", "const/MS", "continue/EGDS",
	"default/DMS", "defer/DS", "else/MS", "fallthrough/MS", "for/H", "func/MS",
	"go/JMRHZGS", "goto/MS", "if/SM", "import/UZGBSMDR", "interface/MGDS",
	"map/ADGJS", "package/AGDS", "range/CGDS", "return/DMS", "select/CSGVD",
	"struct/MS", "switch/MDRSZGB", "type/UAGDS", "var/MS",

	// Built-in
	"append/GDS", "cap/SMDRBZ", "cgo", "copy/ADSG", "goroutine", "goroutines",
	"init/MS", "len", "make/UAGS", "new/STMRYP", "nil/M", "panic/SM",
	"print/AMDSG", "println", "recover/USD",

	// Built-in types
	"bool/MS",
	"int/MS", "int8/MS", "int16/MS", "int32/MS", "int64/MS",
	"uint/MS", "uint8/MS", "uint16/MS", "uint32/MS", "uint64/MS", "uintptr/MS",
	"float32/MS", "float64/MS",
	"complex64/MS", "complex128/MS",
	"string/MDRSZG", "byte/MS", "rune/MS",

	// Commonly used words
	"allocator/MS", "arg/MS", "async", "asm", "boolean/MS", "unbuffer/D",
	"codec/MS", "endian", "endianness", "export/UBSZGMDR", "gcc/M", "glob/SDG",
	"globbing", "hostname/MS", "http/S", "libc/M", "localhost", "mutex/MS",
	"NaN/S", "rpc/MS", "stderr/M", "stdin/M", "stdout/M", "symlink/MS",
	"toolchain/MS", "ascii", "backquote/MS", "charset/MS", "codepoint/MS",
	"comment/UMSGD", "config/MS", "env/MS", "error/DSM", "escaped/UDLMGS",
	"export/UBSZGMDR", "filesystem/MS", "hacky", "hash/RAMDSG", "html/M",
	"intrinsics", "lex/GD", "lossy", "portably", "setting/U", "substring/MS",
	"syscall/MS", "tokenize", "vendor/MSD",

	// Units
	"KiB/S", "MiB/S", "GiB/S", "TiB/S",

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
	"bitbucket/M", "github/M", "gitlab/M", "sourcehut/M", "sr", "ht",
}
