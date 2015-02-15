// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"appengine/datastore"
	"github.com/gorilla/mux"
	"github.com/maruel/go-lepton/appengine/seeall/api"
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
	log.Printf("Error:%s", err)
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

func pushHdlr(w http.ResponseWriter, r *http.Request) {
	req := &api.PushRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		errorJSON(w, err, http.StatusBadRequest)
		return
	}
	n := goon.NewGoon(r)
	src := &Source{ID: req.ID}
	log.Printf("ID:%d Secret:%s; %d imgs", req.ID, base64.URLEncoding.EncodeToString(req.Secret), len(req.Items))
	if err := n.Get(src); err != nil {
		errorJSON(w, err, http.StatusNotFound)
		return
	}

	// TODO(maruel): r.RemoteAddr against src.WhitelistIP.

	// TODO(maruel): Use an HMAC instead of dumb pass around.
	if !bytes.Equal(src.Secret, req.Secret) {
		errorJSON(w, errors.New("incorrect Secret"), http.StatusBadRequest)
		return
	}

	if len(req.Items) == 0 {
		errorJSON(w, errors.New("no item"), http.StatusBadRequest)
		return
	}

	is := &ImageStream{ID: 1, Parent: n.Key(src)}
	imgs := make([]Image, len(req.Items))
	now := time.Now().UTC()
	for i, item := range req.Items {
		imgs[i].Created = now
		imgs[i].RemoteAddr = r.RemoteAddr
		imgs[i].Timestamp = item.Timestamp
		imgs[i].PNG = item.PNG
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
		// Sadly goon supports only one entity type per call, so do two concurrent
		// calls.
		var wg sync.WaitGroup
		var err1 error
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err1 = n.Put(is)
		}()
		var err2 error
		wg.Add(1)
		go func() {
			_, err2 = n.PutMulti(imgs)
		}()
		wg.Wait()
		if err1 != nil {
			return err1
		}
		return err2
	}, opts); err != nil {
		errorJSON(w, err, http.StatusInternalServerError)
		return
	}
	returnJSON(w, &struct{ OK bool }{true})
}
