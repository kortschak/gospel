<!-- Code generated by "go generate" in github.com/kortschak/gospel; DO NOT EDIT. -->
# `gospel` [![Build status](https://github.com/kortschak/gospel/workflows/Test/badge.svg)](https://github.com/kortschak/gospel/actions)

The `gospel` program lints Go source files for misspellings in comments,
strings and embedded files.

It uses hunspell to identify misspellings and makes use of source code
information to reduce the rate of false positive spelling errors where
words refer to labels within the source code.


## Installation

Beyond the standard Go installation process, you must also have libhunspell
and its header files on your system. For a debian-based system this is done
with `sudo apt install libhunspell-dev`.


## Work Flow

`gospel` makes use of module information, and so must be run within a module
and will only consider source files that are referred to by the module.

`gospel` can be used without configuration in many cases, particularly for
small source trees where the number of potential errors is likely to be
small. However, its behaviour can be tuned with [configuration files](#configuration-files).
The initial setup workflow makes use of the [`.words`](#.words) file to
build a dictionary of candidate words to ignore.

```
$ gospel -misspellings=.words ./...
```

This will collate all the found misspellings in the source tree and write
them to `.words`. If `gospel` is run again, it will accept the all words in
the source tree as correctly spelled as they now appear in the projects
dictionary. Since hunspell dictionaries are just text files, you can go
through the list of words to remove properly identified misspellings and
leave in words that are domain-specific and incorrectly marked.

For example the `.words` file at the root of the `gospel` repo includes
words that would otherwise be flagged with the default "en_US" spelling
dictionary and was autogenerated using the command above.

```
8
behaviour
colour
coloured
emph
gendoc
hosters
initialisms
pluralisation
```

This file can also be edited to make use of more advanced hunspell
features. See [Hunspell Dictionaries](#hunspell-dictionaries) below.


## Command Line Options

`gospel` provides a number of command line options. The full list can
be obtained by running `gospel -h`, but the majority correspond directly
to [`.gospel.conf` options](.gospel.conf).

The remaining options are not intended to be persistently stored:

- `-config` — whether to use config file (default true, intended for debugging use).
- `-dict-paths` — a colon-separated directory list containing hunspell dictionaries (defaults to a system-specific value).
- `-entropy-filter` — filter strings and embedded files by entropy.
- `-misspellings` — a file path to write a dictionary of misspellings to (see [Work Flow](#work-flow) above).
- `-since` — a git ref specifying that only changes since then should be considered for misspelling (requires git).
- `-update-dict` — whether the `-misspellings` flag is being used to update a dictionary that already exists.
- `-write-config` — emit a config file based on flags and existing config to stdout and exit.


## Configuration Files

`gospel` uses two configuration file types, `.words` files at the module
roots of packages that are being checked, and a `.gospel.conf` file at the
module root of the module in which `gospel` was invoked.

If `gospel` is being used in CI, these files should be committed to the
repository.


### `.words`

The `.words` file is a hunspell `.dic` formatted file. At its simplest, this
is a plain text file with a list of words, one word per line and an initial
word count on the first line.

`gospel` will read _all_ `.words` files found at module roots for the
packages that are being checked and build a dictionary from them to give to
hunspell for the spelling checks.

Hunspell dictionaries are able to express more than just word matches though,
and are able to indicate some grammatically related sets of words based on
rules. This is covered lightly [below](#hunspell-dictionaries).


### `.gospel.conf`

Runtime behaviour of `gospel` can be modified in a persistent way through the
TOML format `.gospel.conf` file. A number of options are provided:

- `ignore_idents` — whether to include syntax information from the source code in the dictionary of acceptable words.
- `lang` — the language tag to specify language locale.
- `show` — whether to show context for identified misspellings.
- `check_strings` — whether to check string literals.
- `check_embedded` — whether to check spelling in files embedded using `//go:embed`.
- `ignore_upper` — whether to ignore words that are all uppercase or their plurals.
- `ignore_single` — whether to ignore single rune words.
- `ignore_numbers` — whether to ignore number literals.
- `read_licenses` — whether to ignore words found in license files.
- `read_git_log` — whether to ignore author names and emails found in the output of `git log` (requires git to be installed, and gospel to be invoked from within a git repository to have any effect).
- `mask_flags` — whether words that could be command-line flags should be removed prior to checking.
- `mask_urls` — whether URLs should be removed prior to checking.
- `check_urls` — whether the HTTP/HTTPS reachability of URLs should be checked.
- `camel` — whether to split camelCase words into the components if the complete word is not accepted, otherwise split only on underscore.
- `max_word_len` — the maximum length of words that should be checked.
- `min_naked_hex` — minimum length for exclusion of words that are composed of only hex digits 0-9 and a-f (case insensitive).
- `suggest` — when suggestions should be presented for misspellings: "never", "once", once for "each" comment block, or "always".
- `diff_context` — how many lines around a change should be checked when the `-since` flag is used.
- `entropy_filter` — controls the entropy filter used to exclude non-natural language from checking.
    - `min_len_filtered` — the minimum length of text chunks to be considered by the entropy filter; the string literal length for strings, the file length for embedded files and the line or block length for comments.
    - `entropy_filter.accept` — the range of complexity to allow as natural language for checking and roughly corresponds to the effective alphabet size for the language.

The `.gospel.conf` file is intended to set base behaviour that can be
modified with the command line flags during tuning.

The current default `.gospel.conf` file looks like this:

```toml
ignore_idents = true
lang = "en_US"
show = true
check_strings = false
check_embedded = false
ignore_upper = true
ignore_single = true
ignore_numbers = true
read_licenses = true
read_git_log = true
mask_flags = false
mask_urls = true
check_urls = false
camel = true
max_word_len = 40
min_naked_hex = 8
suggest = "never"
diff_context = 0

[entropy_filter]
  filter = false
  min_len_filtered = 16
  [entropy_filter.accept]
    low = 14
    high = 20
```

## Hunspell Dictionaries

Hunspell dictionaries are composed of two parts, a word list and an affix
definition file. These are briefly described below. More information can be
found in `man 5 hunspell`.

### `.dic` Files

The `.dic` file comprises what you would normally think of as a dictionary.
It is a list of words, one word per line, with an initial hint to hunspell
indicating how many words it should expect to work with. The value of the
hint is not particularly important except for performance, but must be
greater than zero.

In addition to the words, hunspell allows the dictionary to encode "affix rules".
These describe how word roots can be extended to allow related words to be
matched as correct, for example "thing" and "things", or "lint" and "linting".
The affix rules are indicated by a set of characters following the word and
separated by a slash. The two examples here would be represented (in the
"en_US" case) by
```
lint/G
thing/S
```
where the `/S` indicates that "thing" can be pluralised and the "lint" can be
extended to its gerund. It is possible to specify more than one affix rule,
and affixes can be prefix or suffix modifiers. Prefix and suffix rules can
interact as the cross product (see below). So from the "en_US" dictionary,
```
advise/LDRSZGB
advised/UY
```
will match "advise" (root), "advisement" (L), "advised" (D), "adviser" (R),
"advises" (S), "advisers" (Z), "advising" (G) and "advisable" (B) from the
first rule, and "advised" (root), "unadvised" (U), "advisedly" (Y) and
"unadvisedly" (UY) from the second.

### `.aff` Files

Different natural languages use different inflection constructions for
encoding grammatical information and this is specified for each language
in hunspell's `.aff` files.

Again using the en_US language locale, an example of an affix rule definition
(the Z from "advise" above)
```
SFX Z Y 4
SFX Z   0     rs         e
SFX Z   y     iers       [^aeiou]y
SFX Z   0     ers        [aeiou]y
SFX Z   0     ers        [^ey]
```
The first line indicated that the rule is the application of a suffix
(SFX), the rule name is "Z", that the rule can be combined with prefixes
via cross product (Y) and that there are 4 ways to apply the rule.

The remaining lines indicate how each application of the rule should be
applied, and when. The first last field specifies when each rule can be
applied matching to the target word. The second last indicates the suffix to
add, and the third last indicates any characters to remove before adding the
suffix ("0" indicated no removal). So with "advise", the first rule matches,
resulting in "advise"+"r" being accepted as a correctly spelling word. The
words "buy" and "fly" illustrate how the second and third rules would be
applied; "buy" will match the third rule and so would allow "buy"+"ers",
but "fly" would match the second and allow "fl"+"iers".

If you are adding specific rules to your `.words` file, the `.aff` files
for your system can be found in the hunspell dictionary path for reference.
Invoking `hunspell -D` will print the search path and show which dictionaries
are available. On many linux system the files are found in `/usr/share/hunspell/`
and on macos they are usually expected to be in `~/Library/Spelling/` or
`/Library/Spelling/`.

`gospel` will not add affix rules to words that have been identified as
misspelled, but will retain rules that have been added during dictionary
updates.

`.aff` files include other information as well including common misspellings
and how to handle things like ordinal numbers.