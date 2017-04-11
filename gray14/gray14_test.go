// Copyright 2017 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package gray14

import (
	"image"
	"testing"
)

func TestMin(t *testing.T) {
	i := image.NewGray16(image.Rect(0, 0, 1, 1))
	if m := Min(i); m != 65535 {
		t.Fatal(m)
	}
}
