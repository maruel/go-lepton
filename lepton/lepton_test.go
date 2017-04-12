// Copyright 2017 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package lepton

import (
	"testing"

	"periph.io/x/periph/conn/i2c/i2ctest"
	"periph.io/x/periph/conn/spi/spitest"
)

func TestNew(t *testing.T) {
	i := i2ctest.Record{}
	s := spitest.Record{}
	_, err := New(&s, &i, nil)
	if err == nil {
		t.Fatal("implement CS")
	}
	/*
		if err := d.Halt(); err != nil {
			t.Fatal(err)
		}
	*/
}
