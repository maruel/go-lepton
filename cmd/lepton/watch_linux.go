// Copyright 2016 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"os"

	"github.com/maruel/interrupt"
	fsnotify "gopkg.in/fsnotify.v1"
)

func watchFile() error {
	fileName, err := os.Executable()
	if err != nil {
		return err
	}
	fi, err := os.Stat(fileName)
	if err != nil {
		return err
	}
	mod0 := fi.ModTime()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	if err = watcher.Add(fileName); err != nil {
		return err
	}
	for {
		select {
		case <-interrupt.Channel:
			return err
		case err = <-watcher.Errors:
			return err
		case <-watcher.Events:
			if fi, err = os.Stat(fileName); err != nil || !fi.ModTime().Equal(mod0) {
				return err
			}
		}
	}
}
