// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore
// +build ignore

package main

import (
	"log"
	"os"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
)

var docTemplate = template.Must(template.ParseFiles("README.tmpl.md"))

func main() {
	var buf strings.Builder
	err := toml.NewEncoder(&buf).Encode(defaults)
	if err != nil {
		log.Fatalf("could not encode config: %v", err)
	}
	words, err := os.ReadFile(".words")
	if err != nil {
		log.Fatalf("could not read .words file: %v", err)
	}

	parts := map[string]string{
		"warning": `<!-- Code generated by "go generate" in github.com/kortschak/gospel; DO NOT EDIT. -->`,
		"config":  buf.String(),
		"words":   string(words),
	}
	f, err := os.Create("README.md")
	if err != nil {
		log.Fatalf("could not create README.md: %v", err)
	}
	err = docTemplate.Execute(f, parts)
	if err != nil {
		log.Fatalf("could not write README.md: %v", err)
	}
	err = f.Close()
	if err != nil {
		log.Fatalf("failed to close README.md: %v", err)
	}
}
