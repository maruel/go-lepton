// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"crypto/rand"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/user"
)

func init() {
	http.HandleFunc("/", handler)
	http.HandleFunc("/restricted/sources", sourcesHdlr)
	http.HandleFunc("/restricted/sources/add", sourcesAddHdlr)
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Hello, world!")
}

var sourcesTmpl = template.Must(template.New("sources").Parse(`
<html>
  <head>
    <title>See All Sources</title>
  </head>
  <body>
		<h1>Sources</h1>
		<ul>
		{{range $index, $source := .Sources}}
			<li>
				{{$source.Who}} - {{$source.Created}} - {{$source.Name}} - {{$source.Details}} - {{$source.SecretKeyBase64}} - {{$source.IP}}
				<form action="/restricted/source/{{index .Keys $index}}/delete" method="POST">
					<input type="submit" value="Delete">
				</form>
			</li>
    {{end}}
		</ul>
		<form action="/restricted/sources/add" method="POST">
			Name:<input type="text" name="Name"></input><br>
			Description:<input type="text" name="Description"></input><br>
			IP:<input type="text" name="IP"></input><br>
			<input type="submit" value="Add">
		</form>
  </body>
</html>
`))

func sourcesHdlr(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
		return
	}

	q := datastore.NewQuery("Source").Order("__key__")
	data := struct {
		SourceKeys []*datastore.Key
		Sources    []Source
	}{}
	keys, err := q.GetAll(c, &data.Sources)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.SourceKeys = keys
	if err := sourcesTmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func sourcesAddHdlr(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Only POST is supported", http.StatusMethodNotAllowed)
		return
	}
	c := appengine.NewContext(r)
	u := user.Current(c)

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dummy := &Source{}
	source := &Source{
		Who:     u.String(),
		Created: time.Now().UTC(),
		Name:    r.FormValue("Name"),
		Details: r.FormValue("Details"),
		Secret:  random,
		IP:      r.FormValue("IP"),
	}
	for i := int64(1); ; i++ {
		// TODO(maruel): datastore.RunInTransaction()
		key := datastore.NewKey(c, "Source", "", i, nil)
		if err := datastore.Get(c, key, dummy); err != datastore.ErrNoSuchEntity {
			continue
		}
		if _, err := datastore.Put(c, key, source); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		break
	}
	http.Redirect(w, r, "/restricted/sources", http.StatusFound)
}
