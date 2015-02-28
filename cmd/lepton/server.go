// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package main

// Packages the static files in a .go file.
//go:generate go run package/main.go

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/maruel/go-lepton/lepton"
	"github.com/maruel/interrupt"
	"golang.org/x/net/websocket"
)

type WebServer struct {
	cond      sync.Cond
	state     string
	images    [9 * 10]*lepton.LeptonBuffer // 10 seconds worth of images. Each image is ~10kb.
	lastIndex int                          // Index of the most recent image.
}

func (s *WebServer) AddImg(img *lepton.LeptonBuffer) {
	s.cond.L.Lock()
	defer s.cond.L.Unlock()
	s.lastIndex = (s.lastIndex + 1) % len(s.images)
	s.images[s.lastIndex] = img
	s.cond.Broadcast()
}

func StartWebServer(port int) *WebServer {
	w := &WebServer{
		cond:      *sync.NewCond(&sync.Mutex{}),
		lastIndex: -1,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", w.root)
	mux.HandleFunc("/favicon.ico", w.favicon)
	mux.Handle("/stream", websocket.Handler(w.stream))
	fmt.Printf("Listening on %d\n", port)
	go http.ListenAndServe(fmt.Sprintf(":%d", port), loggingHandler{mux})
	go func() {
		<-interrupt.Channel
		w.cond.Broadcast()
	}()
	return w
}

func (s *WebServer) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	s.cond.L.Lock()
	defer s.cond.L.Unlock()
	if _, err := w.Write(read("root.html")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *WebServer) favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "Cache-Control:public, max-age=2592000") // 30d
	w.Write(read("photo_ir.png"))
}

// stream sends all images as PseudoRGB as WebSocket frames.
func (s *WebServer) stream(w *websocket.Conn) {
	log.Printf("websocket from %#v", w)
	defer w.Close()
	lastIndex := 0
	buf := &bytes.Buffer{}
	s.cond.L.Lock()
	defer s.cond.L.Unlock()
	for !interrupt.IsSet() {
		s.cond.Wait()
		for ; !interrupt.IsSet() && lastIndex != s.lastIndex; lastIndex = (lastIndex + 1) % len(s.images) {
			// Frame I is for Image.
			buf.Write([]byte("I"))
			img := s.images[s.lastIndex]
			s.cond.L.Unlock()
			// Do the actual I/O without the lock.
			encoder := base64.NewEncoder(base64.StdEncoding, buf)
			var err error
			if err = png.Encode(encoder, img.AGCRGBLinear()); err == nil {
				encoder.Close()
				_, err = w.Write(buf.Bytes())
			}
			buf.Reset()
			// Frame M is for Metadata.
			if err == nil {
				buf.Write([]byte("M"))
				// Note: time.Duration and CentiC are sent as raw, which is less nice
				// but easier to process.
				err = json.NewEncoder(buf).Encode(&img.Metadata)
				if err == nil {
					_, err = w.Write(buf.Bytes())
				}
				buf.Reset()
			}

			s.cond.L.Lock()
			// To break out of the loop, the lock must be held.
			if err != nil {
				log.Printf("websocket err: %s", err)
				break
			}
		}
	}
}

// Private details.

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

// Hijack is needed for websocket.
func (l *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h := l.ResponseWriter.(http.Hijacker)
	return h.Hijack()
}

// ServeHTTP logs each HTTP request if -verbose is passed.
func (l loggingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lrw := &loggingResponseWriter{ResponseWriter: w}
	l.handler.ServeHTTP(lrw, r)
	log.Printf("%s - %3d %6db %4s %s\n", r.RemoteAddr, lrw.status, lrw.length, r.Method, r.RequestURI)
}
