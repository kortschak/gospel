// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var diffTests = []struct {
	commit string
	diff   string
	want   map[string][]lineRange
}{
	{
		commit: "initial code",
		diff: `diff --git a/main.go b/main.go
new file mode 100644
index 0000000..a3dd973
--- /dev/null
+++ b/main.go
@@ -0,0 +1,7 @@
+package main
+
+import "fmt"
+
+func main() {
+	fmt.Println("Hello, World!")
+}
`,
		want: map[string][]lineRange{
			"main.go": {
				{start: 1, end: 7},
			},
		},
	},
	{
		commit: "initial errors",
		diff: `diff --git a/main.go b/main.go
index a3dd973..d74ba56 100644
--- a/main.go
+++ b/main.go
@@ -3,5 +3,5 @@ package main
 import "fmt"
 
 func main() {
-	fmt.Println("Hello, World!")
+	fmt.Println("Hulloo, Wurld!")
 }
`,
		want: map[string][]lineRange{
			"main.go": {
				{start: 3, end: 7},
			},
		},
	},
	{
		commit: "initial errors",
		diff: `diff --git a/main.go b/main.go
index a3dd973..d74ba56 100644
--- a/main.go
+++ b/main.go
@@ -3,5 +3,5 @@ package main
 import "fmt"
 
 func main() {
-	fmt.Println("Hello, World!")
+	fmt.Println("Hulloo, Wurld!")
 }
`,
		want: map[string][]lineRange{
			"main.go": {
				{start: 3, end: 7},
			},
		},
	},
	{
		commit: "add more common words to known words list -U0",
		diff: `diff --git a/known_words.go b/known_words.go
index 4c65bdc..f2b1833 100644
--- a/known_words.go
+++ b/known_words.go
@@ -39 +39,7 @@ var knownWords = []string{
-       "KiB/S", "MiB/S", "GiB/S", "TiB/S",
+       "Å/S", "nm/S", "µm/S", "mm/S", "cm/S", "m/S", "km/S",
+       "ns", "µs", "ms", "s", "min/S", "hr/S",
+       "Hz",
+       "Kb/S", "kb/S", "Mb/S", "Gb/S", "Tb/S",
+       "KB/S", "kB/S", "MB/S", "GB/S", "TB/S",
+       "Kib/S", "kib/S", "Mib/S", "Gib/S", "Tib/S",
+       "KiB/S", "kiB/S", "MiB/S", "GiB/S", "TiB/S",
@@ -41,4 +47,5 @@ var knownWords = []string{
-       // Architectures
-       "aarch", "aix", "amd", "amd64", "arm64", "darwin", "freebsd", "illumos", "ios",
-       "js", "linux", "mips", "mips64", "mips64le", "mipsle", "netbsd", "openbsd",
-       "ppc64", "ppc64le", "riscv64", "s390x", "solaris", "wasm", "windows",
+       // Architectures and operating systems
+       "aarch", "aix", "amd", "amd64", "arm64", "bsd", "darwin", "freebsd", "illumos",
+       "ios", "iOS", "js", "linux", "mips", "mips64", "mips64le", "mipsle", "netbsd",
+       "openbsd", "plan9", "ppc64", "ppc64le", "riscv64", "s390x", "solaris", "wasm",
+       "windows",
@@ -122,0 +130 @@ var knownWords = []string{
+       "deadcode",
@@ -248,0 +257 @@ var knownWords = []string{
+       "pthread/S",
`,
		want: map[string][]lineRange{
			"known_words.go": {
				{start: 39, end: 45},
				{start: 47, end: 51},
				{start: 130, end: 130},
				{start: 257, end: 257},
			},
		},
	},
	{
		commit: "remove duplicate word -U0",
		diff: `diff --git a/known_words.go b/known_words.go
index e24d35e..4c65bdc 100644
--- a/known_words.go
+++ b/known_words.go
@@ -233 +232,0 @@ var knownWords = []string{
-       "pragma",
`,
		want: map[string][]lineRange{},
	},
	{
		commit: "remove duplicate word -U0",
		diff: `diff --git a/known_words.go b/known_words.go
index e24d35e..4c65bdc 100644
--- a/known_words.go
+++ b/known_words.go
@@ -230,7 +230,6 @@ var knownWords = []string{
        "portably",
        "postcondition",
        "pragma",
-       "pragma",
        "preallocate/DSG",
        "precalculated",
        "precalculation",
`,
		want: map[string][]lineRange{
			"known_words.go": {
				{start: 230, end: 235},
			},
		},
	},
}

func TestAdditions(t *testing.T) {
	for _, test := range diffTests {
		got, err := additions(strings.NewReader(test.diff))
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("unexpected result for test %q\n%s",
				test.commit, cmp.Diff(got, test.want),
			)
		}
	}
}
