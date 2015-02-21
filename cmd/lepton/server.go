// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"image/png"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/mux"
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
				still.src = "/still/rgb/latest.png#" + new Date().getTime();
			}

			function loadStats() {
				// Do AJAX stuff.
				Uint16Array();
			}

			function tmp() {
				var can = document.getElementById('canvas1');
				var context = can.getContext('2d');
				context.clearRect(0, 0, image.width, image.height);
				var drawing = new Image();
				drawing.onload = function() {
					context.drawImage(drawing, 0, 0);
				};
				drawing.src = "/still/rgb/latest.png#" + new Date().getTime();
			}
		</script>
	</head>
	<body>
		Still:<br>
		<a href="/still/rgb/latest.png"><img class="large" id="still" src="/still/rgb/latest.png" onload="reload()"></img></a>
		<img src="/still/rgb/palette/v.png" width="60px" height="256px" border=1></img>
		<br>
		TODO(maruel): Add JS updated stats.
		<canvas id="canvas1" width="500" height="500"></canvas>
	</body>
	</html>`))

type WebServer struct {
	lock      sync.Mutex
	state     string
	images    [9 * 10]*lepton.LeptonBuffer // 10 seconds worth of images. Each image is ~10kb.
	lastIndex int                          // Index of the most recent image.
}

func (s *WebServer) AddImg(img *lepton.LeptonBuffer) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.lastIndex = (s.lastIndex + 1) % len(s.images)
	s.images[s.lastIndex] = img
}

func StartWebServer(port int) *WebServer {
	w := &WebServer{lastIndex: -1}
	mux := mux.NewRouter()
	mux.HandleFunc("/", w.root)
	mux.HandleFunc("/favicon.ico", w.stillGrayLatestPNG)
	mux.HandleFunc("/still/gray/diff.png", w.stillGrayDiffPNG)
	mux.HandleFunc("/still/gray/latest.png", w.stillGrayLatestPNG)
	mux.HandleFunc("/still/gray/palette/{orientation:[hv]}.png", w.stillGrayPalettePNG)
	mux.HandleFunc("/still/gray/{id:[0-9]+}.png", w.stillGrayPNG)
	mux.HandleFunc("/still/rgb/diff.png", w.stillRGBDiffPNG)
	mux.HandleFunc("/still/rgb/latest.png", w.stillRGBLatestPNG)
	mux.HandleFunc("/still/rgb/palette/{orientation:[hv]}.png", w.stillRGBPalettePNG)
	mux.HandleFunc("/still/rgb/{id:[0-9]+}.png", w.stillRGBPNG)
	mux.HandleFunc("/still/{id:[0-9]+}.json", w.stillJSON)
	mux.HandleFunc("/still/latest.json", w.stillLatestJSON)
	fmt.Printf("Listening on %d\n", port)
	go http.ListenAndServe(fmt.Sprintf(":%d", port), loggingHandler{mux})
	return w
}

func (s *WebServer) root(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	s.lock.Lock()
	if err := rootTmpl.Execute(w, s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	s.lock.Unlock()
}

func (s *WebServer) stillGrayPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	if err := png.Encode(w, s.getImage(getID(r)).AGCGrayLinear()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) stillGrayLatestPNG(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("/still/gray/%d.png", s.getLatest()), http.StatusFound)
}

func (s *WebServer) stillGrayDiffPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	latest := s.getLatest()
	if err := png.Encode(w, s.getImage(latest).DiffGray(s.getImage(latest-1))); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) stillGrayPalettePNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "Cache-Control:public, max-age=600")
	if err := png.Encode(w, lepton.PaletteGray(mux.Vars(r)["orientation"] == "v")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) stillRGBPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	if err := png.Encode(w, s.getImage(getID(r)).AGCRGBLinear()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) stillRGBLatestPNG(w http.ResponseWriter, r *http.Request) {
	//http.Redirect(w, r, fmt.Sprintf("/still/rgb/%d.png", s.getLatest()), http.StatusFound)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	id := s.getLatest()
	if err := png.Encode(w, s.getImage(id).AGCRGBLinear()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) stillRGBDiffPNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	latest := s.getLatest()
	if err := png.Encode(w, s.getImage(latest).DiffRGB(s.getImage(latest-1))); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) stillRGBPalettePNG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "Cache-Control:public, max-age=600")
	if err := png.Encode(w, lepton.PaletteRGB(mux.Vars(r)["orientation"] == "v")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) stillJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.getImage(getID(r))); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) stillLatestJSON(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("/still/%d.json", s.getLatest()), http.StatusFound)
}

// Private details.

func getID(r *http.Request) int {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		panic("internal error")
	}
	return id
}

func (s *WebServer) getLatest() int {
	s.lock.Lock()
	defer s.lock.Unlock()
	//return s.lastIndex
	if s.lastIndex == -1 {
		return 0
	}
	return int(s.images[s.lastIndex].FrameCount)
}

func (s *WebServer) getImage(id int) *lepton.LeptonBuffer {
	if s.lastIndex == -1 {
		return &lepton.LeptonBuffer{}
	}
	id32 := uint32(id)
	s.lock.Lock()
	defer s.lock.Unlock()
	//return s.images[s.lastIndex]
	for _, img := range s.images {
		if img.FrameCount == id32 {
			return img
		}
	}
	return &lepton.LeptonBuffer{}
}

type loggingHandler struct {
	handler http.Handler
}

type loggingResponseWriter struct {
	http.ResponseWriter
	length int
	status int
}

func (l *loggingResponseWriter) Write(data []byte) (size int, err error) {
	size, err = l.ResponseWriter.Write(data)
	l.length += size
	return
}

func (l *loggingResponseWriter) WriteHeader(status int) {
	l.ResponseWriter.WriteHeader(status)
	l.status = status
}

// Logs each HTTP request if -verbose is passed.
func (l loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lrw := &loggingResponseWriter{ResponseWriter: w}
	l.handler.ServeHTTP(lrw, r)
	log.Printf("%s - %3d %6db %4s %s\n", r.RemoteAddr, lrw.status, lrw.length, r.Method, r.RequestURI)
}
