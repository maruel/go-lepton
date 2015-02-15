// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"appengine/datastore"
	"github.com/gorilla/mux"
	"github.com/mjibson/goon"
)

func apiRoute(r *mux.Router) {
	r.HandleFunc("/api/seeall/v1/push", jsonAPI(pushHdlr))
}

func returnJSON(w http.ResponseWriter, ret interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(ret); err != nil {
		panic(err)
	}
}

func errorJSON(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	ret := &struct{ Error error }{err}
	if err := json.NewEncoder(w).Encode(ret); err != nil {
		panic(err)
	}
}

func jsonAPI(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			errorJSON(w, errors.New("Only POST is supported"), http.StatusMethodNotAllowed)
			return
		}
		ct, ok := r.Header["Content-Type"]
		if !ok || len(ct) != 1 || ct[0] != "application/json" {
			errorJSON(w, errors.New("Requires Content-Type: application/json"), http.StatusBadRequest)
			return
		}
		f(w, r)
	}
}

type PushRequestItem struct {
	Timestamp time.Time
	PNG       []byte
}

type PushRequest struct {
	ID     int64
	Secret string
	Items  []PushRequestItem
}

func pushHdlr(w http.ResponseWriter, r *http.Request) {
	req := &PushRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		errorJSON(w, err, http.StatusBadRequest)
		return
	}
	n := goon.NewGoon(r)
	src := &Source{ID: req.ID}
	if err := n.Get(src); err != nil {
		errorJSON(w, err, http.StatusNotFound)
		return
	}

	// TODO(maruel): r.RemoteAddr against src.WhitelistIP.

	if src.SecretBase64() != req.Secret {
		errorJSON(w, errors.New("incorrect Secret"), http.StatusBadRequest)
		return
	}

	if len(req.Items) == 0 {
		errorJSON(w, errors.New("no item"), http.StatusBadRequest)
		return
	}

	is := &ImageStream{ID: 1, Parent: n.Key(src)}
	imgs := make([]Image, len(req.Items))
	entities := make([]interface{}, len(req.Items)+1)
	entities[0] = is
	now := time.Now().UTC()
	for i, item := range req.Items {
		imgs[i].Created = now
		imgs[i].RemoteAddr = r.RemoteAddr
		imgs[i].Timestamp = item.Timestamp
		imgs[i].PNG = item.PNG
		entities[i+1] = imgs[i]
	}
	opts := &datastore.TransactionOptions{}
	if err := n.RunInTransaction(func(tg *goon.Goon) error {
		if err := tg.Get(is); err != nil && err != datastore.ErrNoSuchEntity {
			return err
		}
		key := tg.Key(is)
		for i := range imgs {
			imgs[i].ID = is.NextID
			imgs[i].Parent = key
			is.NextID++
		}
		is.NextID++
		is.Modified = now
		if _, err := n.PutMulti(entities); err != nil {
			return err
		}
		return nil
	}, opts); err != nil {
		errorJSON(w, err, http.StatusInternalServerError)
		return
	}
	returnJSON(w, &struct{ OK bool }{true})
}
