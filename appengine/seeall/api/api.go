// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package api

import (
	"time"
)

type PushRequestItem struct {
	Timestamp time.Time
	PNG       []byte
}

type PushRequest struct {
	ID     int64
	Secret []byte
	Items  []PushRequestItem
}
