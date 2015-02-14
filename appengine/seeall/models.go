// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"encoding/base64"
	"time"
)

type GlobalConfig struct {
}

// Key is an id starting a 1.
type Source struct {
	Who     string    `datastore:""`
	Created time.Time `datastore:""`
	Name    string    `datastore:""`
	Details string    `datastore:",noindex"`
	Secret  []byte    `datastore:",noindex"`
	IP      string    `datastore:",noindex"`
}

func (s *Source) SecretKeyBase64() string {
	return base64.URLEncoding.EncodeToString(s.Secret)
}

type Image struct {
	Who     string    `datastore:""`
	Created time.Time `datastore:""`
	PNG     []byte    `datastore:"noindex"`
}
