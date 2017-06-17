// Copyright 2016 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

//+build !linux

package main

import "github.com/maruel/interrupt"

func watchFile() error {
	<-interrupt.Channel
	return nil
}
