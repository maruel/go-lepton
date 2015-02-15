// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"crypto/rand"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/user"
	"github.com/mjibson/goon"
)

func init() {
	http.HandleFunc("/restricted/sources", sourcesHdlr)
	http.HandleFunc("/restricted/sources/add", sourcesAddHdlr)
	http.HandleFunc("/restricted/source/", sourceHdlr)
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
				{{$source.Who}} - {{$source.Created}} - {{$source.Name}} - {{$source.Details}} - {{$source.SecretBase64}} - {{$source.IP}}
				<form action="/restricted/source/{{with index $.SourceKeys $index}}{{.IntID}}{{end}}/delete" method="POST">
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
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
		return
	}

	n := goon.NewGoon(r)
	q := datastore.NewQuery("Source").Order("__key__")
	data := struct {
		SourceKeys []*datastore.Key
		Sources    []Source
	}{}
	keys, err := n.GetAll(q, &data.Sources)
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
	n := goon.NewGoon(r)
	// TODO(maruel): XSRF token.
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
		dummy.ID = i
		if err := n.Get(dummy); err != datastore.ErrNoSuchEntity {
			continue
		}
		source.ID = i
		if _, err := n.Put(source); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		break
	}
	http.Redirect(w, r, "/restricted/sources", http.StatusFound)
}

var reSource = regexp.MustCompile("^/restricted/source/(\\d+)$")
var reSourceDelete = regexp.MustCompile("^/restricted/source/(\\d+)/delete$")

var sourceTmpl = template.Must(template.New("source").Parse(`
<html>
  <head>
    <title>See All Source {{.Source.Name}}</title>
  </head>
  <body>
		<h1>Source {{.Source.Name}}</h1>
		<ul>
		{{range .Images}}
			<img src="data:image/png;base64,{{.PNGBase64}}"></img><br>
    {{end}}
  </body>
</html>
`))

func sourceHdlr(w http.ResponseWriter, r *http.Request) {
	n := goon.NewGoon(r)
	if m := reSource.FindStringSubmatch(r.URL.Path); m != nil {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.Atoi(m[1])
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := struct {
			Images []Image
			Source Source
		}{
			Source: Source{ID: int64(id)},
		}
		if err := n.Get(&data.Source); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		q := datastore.NewQuery("Image").Order("__key__").Ancestor(n.Key(data.Source))
		if _, err := n.GetAll(q, &data.Images); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := sourcesTmpl.Execute(w, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if m := reSourceDelete.FindStringSubmatch(r.URL.Path); m != nil {
		if r.Method != "POST" {
			http.Error(w, "Only POST is supported", http.StatusMethodNotAllowed)
			return
		}
		// TODO(maruel): XSRF token.
		id, err := strconv.Atoi(m[1])
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := n.Delete(n.Key(&Source{ID: int64(id)})); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/restricted/sources", http.StatusFound)
		return
	}
	http.Error(w, "Not Found", http.StatusNotFound)
}
