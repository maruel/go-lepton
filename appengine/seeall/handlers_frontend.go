// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"fmt"
	"html/template"
	"net/http"

	"appengine"
	"appengine/datastore"
	//"appengine/user"
)

func init() {
	http.HandleFunc("/", handler)
	http.HandleFunc("/restricted/sources", sources)
	//http.HandleFunc("/restricted/sources/add", sourcesAdd)
	//http.HandleFunc("/restricted/source/", source)
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
    {{range .Sources}}
			<li>
				{{.Who}} - {{.Created}} - {{.Name}} - {{.Details}} - {{.SecretKey}} - {{.IP}}
				<form action="/restricted/source/TODO_ID/delete" method="POST">
					<input type="submit" value="Delete">
				</form>
			</li>
    {{end}}
		</ul>
  </body>
</html>
`))

func sources(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)

	/* POST
	if u := user.Current(c); u != nil {
		// = u.String()
	}
	key := datastore.NewIncompleteKey(c, "Source", nil)
	_, err := datastore.Put(c, key, &g)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	*/

	// GET
	q := datastore.NewQuery("Source").Order("-__key__")
	data := struct {
		Sources []Source
	}{}
	if _, err := q.GetAll(c, &data.Sources); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	if err := sourcesTmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
