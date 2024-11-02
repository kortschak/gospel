module github.com/kortschak/gospel

go 1.22.0

toolchain go1.23.2

require (
	github.com/BurntSushi/toml v1.0.0
	github.com/google/go-cmp v0.6.0
	github.com/google/licensecheck v0.3.1
	github.com/kortschak/camel v0.0.0-20220208065757-0665e01e7dba
	github.com/kortschak/ct v0.0.0-20140325011614-7d86dffe6951
	github.com/kortschak/hunspell v0.0.0-20220305030544-5d8374a03860
	github.com/rogpeppe/go-internal v1.13.1
	golang.org/x/sys v0.26.0
	golang.org/x/tools v0.26.0
	mvdan.cc/xurls/v2 v2.4.0
)

require (
	golang.org/x/mod v0.21.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
)

retract (
	v1.10.1 // Unsafe use of os/exec.
	v1.10.0 // Unsafe use of os/exec.
)
