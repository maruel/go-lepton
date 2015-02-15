// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"encoding/base64"
	"time"

	"appengine/datastore"
)

type GlobalConfig struct {
}

// Key is an id starting a 1.
type Source struct {
	ID          int64     `datastore:"-" goon:"id"`
	Created     time.Time `datastore:""`
	RemoteAddr  string    `datastore:""`
	Who         string    `datastore:""`
	Name        string    `datastore:""`
	Details     string    `datastore:",noindex"`
	Secret      []byte    `datastore:",noindex"`
	WhitelistIP string    `datastore:",noindex"`
}

func (s *Source) SecretBase64() string {
	return base64.URLEncoding.EncodeToString(s.Secret)
}

// Key is id==1, Parent == Source.
type ImageStream struct {
	ID       int64          `datastore:"-" goon:"id"`
	Parent   *datastore.Key `datastore:"-" goon:"parent"`
	Modified time.Time      `datastore:""`
	NextID   int64          `datastore:""`
}

type Image struct {
	ID         int64          `datastore:"-" goon:"id"`
	Parent     *datastore.Key `datastore:"-" goon:"parent"`
	Created    time.Time      `datastore:""`
	RemoteAddr string         `datastore:""`
	Timestamp  time.Time      `datastore:""`
	PNG        []byte         `datastore:",noindex"`
}

func (i *Image) PNGBase64() string {
	return base64.URLEncoding.EncodeToString(i.PNG)
}
