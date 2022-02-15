// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// knownWords contains a list of commonly encountered words that
// may not be in user dictionaries. It is used to construct a
// temporary dictionary to load into hunspell.
var knownWords = []string{
	"golang/M",

	// Place-holders for rules. This is used to provide pluralisation
	// rules for idents. Included just in case the locale's dictionary
	// doesn't have it.
	"item/MS",

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
	"affine", "allocator/MS", "ansi", "arg/MS", "ascii", "asm", "async", "atomic/S",
	"backquot/SD", "backquote/MS", "bitmask/SD", "bitwise", "boolean/MS",
	"buildmode/S", "canonicalize/S", "charset/MS", "checkmark/S", "codec/MS",
	"codepoint/MS", "comment/UMSGD", "config/MS", "coord/S", "cryptographic",
	"cryptographically", "deallocate/D", "decrypt/SD", "delim/S", "denormal",
	"denormalized", "dereference/DSG", "duration/S", "encode/DG", "encoding/S",
	"endian", "endianness", "env/MS", "error/DSM", "escaped/UDLMGS", "escaper/S",
	"export/UBSZGMDR", "export/UBSZGMDR", "filesystem/MS", "finalizer/S",
	"framepointer/S", "gcc/M", "glibc", "glob/SDG", "global/S", "globbing",
	"godoc", "gofmt/SD", "gzipped", "hacky", "hash/RAMDSG", "hostname/MS",
	"href/S", "html/M", "http/S", "ieee", "ietf", "iff", "indirect/SDNX",
	"initializer/S", "inline/DG", "instantiate/SDX", "interoperability",
	"intrinsics", "invariant/S", "iterative/Y", "latency/S", "lex/GD",
	"lexically", "libc/M", "localhost", "localtime", "lookup/S", "loopback",
	"lossy", "memoization", "memprofile", "multicast", "mutator/S", "mutex/MS",
	"namespace/S", "namespaces", "NaN/S", "poller", "popcount", "portably",
	"preallocate/DSG", "precompute/DSG", "prepend/DSG", "proc/S", "profiler/S",
	"quantization", "readme", "relocation/S", "rescan/D", "rfc", "rpc/MS",
	"scannable", "setting/U", "sha", "stderr/M", "stdin/M", "stdout/M",
	"subdirectory/S", "subexpression/S", "submatch/S", "subproblem/S",
	"subslice/S", "substring/MS", "subtest", "subtree", "symlink/MS",
	"syscall/MS", "tokenize/DRS", "toolchain/MS", "tracebacks/S",
	"typecheck/RDSG", "unaddressable", "unallocated", "unbuffer/D",
	"underflow/S", "unescape/S", "unexport/D", "unicast", "uninstantiated",
	"unlink/D", "unmapped", "unmarshal/DSG", "unread/S", "unscavenged",
	"untrusted", "vendor/D", "vendor/MSD", "whitespace", "workbuf/S",
	"www",

	// Units
	"KiB/S", "MiB/S", "GiB/S", "TiB/S",

	// Architectures
	"aix", "amd", "amd64", "arm64", "darwin", "freebsd", "illumos", "ios",
	"js", "linux", "mips", "mips64", "mips64le", "mipsle", "netbsd", "openbsd",
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
