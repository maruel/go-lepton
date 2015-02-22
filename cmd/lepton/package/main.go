// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"text/template"
)

var contents map[string]string

var tmpl = template.Must(template.New("tmpl").Parse(`// Automatically generated file. Do not edit!
// Generated with "go run package/main.go"

package main

var staticFiles = map[string]string{
{{range $key, $value := .}}	{{$key}}: {{$value}},
{{end}}}
`))

func walk(path string, info os.FileInfo, err error) error {
	if info.IsDir() {
		return nil
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	contents[strconv.Quote(info.Name())] = strconv.Quote(string(data))
	return nil
}

func mainImpl() error {
	contents = map[string]string{}
	if err := filepath.Walk("static", walk); err != nil {
		return err
	}
	f, err := os.Create("static_files_gen.go")
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.Execute(f, contents)
}

func main() {
	if err := mainImpl(); err != nil {
		fmt.Fprintf(os.Stderr, "\npackage: %s.\n", err)
		os.Exit(1)
	}
}
