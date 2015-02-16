// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"fmt"
	"html/template"
	"image"
	"image/png"
	"net/http"
	"sync"

	"github.com/maruel/go-lepton/lepton"
)

var rootTmpl = template.Must(template.New("name").Parse(`
	<html>
	<head>
		<title>go-lepton</title>
		<style>
			img.large {
				width: 480; /* Multiple of 80 */
				height: auto;
			}
		</style>
		<script>
			function reload() {
				var still = document.getElementById("still");
				still.src = "/still.png#" + new Date().getTime();
			}
		</script>
	</head>
	<body>
		Still:<br>
		<a href="/still.png"><img class="large" id="still" src="/still.png" onload="reload()"></img></a>
		<br>
		{{.Stats}}
		<br>
		{{.Img.Min}} - {{.Img.Max}}
	</body>
	</html>`))

type WebServer struct {
	lock    sync.Mutex
	state   string
	lastImg *lepton.LeptonBuffer
}

func (s *WebServer) SetImg(img *lepton.LeptonBuffer) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.lastImg = img
}

func (s *WebServer) root(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	s.lock.Lock()
	if err := rootTmpl.Execute(w, s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	s.lock.Unlock()
}

func (s *WebServer) still(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	img := image.NewGray(image.Rect(0, 0, 80, 60))
	s.lock.Lock()
	s.lastImg.AGC(img)
	s.lock.Unlock()
	if err := png.Encode(w, img); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) still16(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	s.lock.Lock()
	defer s.lock.Unlock()
	if err := png.Encode(w, s.lastImg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func StartWebServer(port int) *WebServer {
	w := &WebServer{}
	http.HandleFunc("/", w.root)
	http.HandleFunc("/favicon.ico", w.still)
	http.HandleFunc("/still.png", w.still)
	http.HandleFunc("/still16.png", w.still16)
	fmt.Printf("Listening on %d\n", port)
	go http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	return w
}
