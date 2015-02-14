// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"time"
)

type GlobalConfig struct {
}

// Key is an id starting a 1.
type Source struct {
	Who       string    `datastore:""`
	Created   time.Time `datastore:""`
	Name      string    `datastore:""`
	Details   string    `datastore:",noindex"`
	SecretKey []byte    `datastore:",noindex"`
	IP        string    `datastore:",noindex"`
}

type Image struct {
	Who     string    `datastore:""`
	Created time.Time `datastore:""`
	PNG     []byte    `datastore:"noindex"`
}
