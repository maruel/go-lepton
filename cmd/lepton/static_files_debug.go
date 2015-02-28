// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// +build debug

package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func read(name string) []byte {
	// HACK. Better idea is welcome.
	gopath := strings.Split(os.Getenv("GOPATH"), string(os.PathListSeparator))[0]
	path := filepath.Join(gopath, "src", "github.com", "maruel", "go-lepton", "cmd", "lepton", "static", name)
	content, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return content
}
