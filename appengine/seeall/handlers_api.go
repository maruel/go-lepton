// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/mjibson/goon"
)

func init() {
	http.HandleFunc("/api/seeall/v1/push", jsonAPI(pushHdlr))
}

func jsonAPI(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Only POST is supported", http.StatusMethodNotAllowed)
			return
		}
		ct, ok := r.Header["Content-Type"]
		if !ok || len(ct) != 1 || ct[0] != "application/json" {
			http.Error(w, "Requires Content-Type: application/json", http.StatusBadRequest)
			return
		}
		f(w, r)
	}
}

func pushHdlr(w http.ResponseWriter, r *http.Request) {
	// TODO(maruel): All results as json.
	req := &struct {
		ID      int64
		Secret  string
		Created time.Time
		PNG     []byte
	}{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	n := goon.NewGoon(r)
	src := &Source{ID: req.ID}
	if err := n.Get(src); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if src.SecretBase64() != req.Secret {
		http.Error(w, "Bad secret", http.StatusBadRequest)
		return
	}
	// TODO(maruel): IP whitelist.

	for i := int64(1); ; i++ {
		// TODO(maruel): datastore.RunInTransaction()
		img := &Image{
			ID:      i,
			Parent:  n.Key(src),
			Created: time.Now().UTC(),
			PNG:     req.PNG,
		}
		if _, err := n.Put(img); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		break
	}
	w.Header().Set("Content-Type", "application/json")
	ret := &struct {
		OK bool
	}{true}
	if err := json.NewEncoder(w).Encode(ret); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
}
