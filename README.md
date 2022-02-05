# `gospel` [![Build status](https://github.com/kortschak/gospel/workflows/Test/badge.svg)](https://github.com/kortschak/gospel/actions)

The `gospel` program lints Go source code for misspellings in comments and strings. It uses hunspell to identify misspellings and only emits coloured output for visual inspection; don't use it in automated linting.

## Installation

Beyond the standard Go installation process, you must also have libhunspell and its header files on your system.